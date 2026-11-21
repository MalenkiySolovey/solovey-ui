//go:build linux && !minimal

package nftables

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/btf"
	"github.com/cilium/ebpf/link"
	"golang.org/x/sys/unix"
)

const (
	zapretGuardPriority = math.MaxInt32 - 1
	zapretSocketLimit   = 1 << 16
)

// ZapretNativeGuard is owned by the installed Planner adapter. It starts
// closed. Pins are kernel lifetime anchors, not authority to adopt an old
// generation. Close deliberately leaves them attached.
type ZapretNativeGuard struct {
	mu                        sync.Mutex
	device, deadline, sockets *ebpf.Map
	hooks                     []zapretGuardPin
	ifindex                   uint32
	namespace                 uint64
	pinned                    bool
	revoked, closed, leased   bool
}

type zapretGuardPin struct {
	link.Link
	path    string
	id      link.ID
	program ebpf.ProgramID
	pinned  bool
}

// CreateZapretNativeGuard is the deny-generator guard used by capability
// probes. The staged path authorizes generator sockets by kernel cookie.
func CreateZapretNativeGuard(plan planner.ZapretPlan, pinDir string) (*ZapretNativeGuard, error) {
	return createZapretNativeGuard(plan, pinDir, [2]uint64{})
}

func createZapretNativeGuard(plan planner.ZapretPlan, pinDir string, raw [2]uint64) (*ZapretNativeGuard, error) {
	g, err := stageZapretNativeGuard(plan, pinDir, raw)
	if err != nil {
		return nil, err
	}
	if err := g.pinLinks(); err != nil {
		return nil, errors.Join(err, g.AbortUnleased())
	}
	return g, nil
}

// Staged deny hooks are held by file descriptors only. If the broker dies
// before the journal commit, the kernel releases them with the broker's FDs.
func stageZapretNativeGuard(plan planner.ZapretPlan, pinDir string, raw [2]uint64) (_ *ZapretNativeGuard, failure error) {
	scope, err := plan.SocketScope()
	if err != nil || plan.FinalRouteBit() == 0 {
		return nil, errors.New("guard for Zapret requires an installed Planner allocation")
	}
	if err := checkZapretPinDirectory(pinDir); err != nil {
		return nil, err
	}
	if strconv.IntSize != 64 {
		return nil, errors.New("BPF guard for Zapret requires a 64-bit Linux kernel and broker")
	}
	probe, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("observe Zapret guard network namespace: %w", err)
	}
	namespace, err := unix.GetsockoptUint64(probe, unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	_ = unix.Close(probe)
	if err != nil || namespace != scope.Egress().NamespaceCookie {
		return nil, errors.New("guard for Zapret is not in the planned network namespace")
	}
	layout, err := observeZapretKernelLayout()
	if err != nil {
		return nil, fmt.Errorf("egress guard for Zapret requires kernel BTF: %w", err)
	}
	g := &ZapretNativeGuard{ifindex: uint32(scope.Egress().IfIndex), namespace: namespace}
	defer func() {
		if failure != nil {
			failure = errors.Join(failure, g.AbortUnleased())
		}
	}()
	g.device, err = ebpf.NewMap(&ebpf.MapSpec{Name: "sui_zapret_dev", Type: ebpf.DevMap, KeySize: 4, ValueSize: 4, MaxEntries: 1})
	if err != nil {
		return nil, fmt.Errorf("create Zapret netdevice map: %w", err)
	}
	if err := g.device.Put(uint32(0), g.ifindex); err != nil {
		return nil, fmt.Errorf("reference the selected Zapret link: %w", err)
	}
	g.deadline, err = ebpf.NewMap(&ebpf.MapSpec{Name: "sui_zapret_ttl", Type: ebpf.Array, KeySize: 4, ValueSize: 8, MaxEntries: 1, Flags: unix.BPF_F_RDONLY_PROG})
	if err != nil {
		return nil, fmt.Errorf("create Zapret deadline map: %w", err)
	}
	// Cookies are kernel identities, not FD numbers or marks. Entries are
	// never evicted or recycled across generations. Capacity exhaustion denies
	// further admissions rather than weakening old-socket containment.
	g.sockets, err = ebpf.NewMap(&ebpf.MapSpec{Name: "sui_zapret_sock", Type: ebpf.Hash, KeySize: 8, ValueSize: 4,
		MaxEntries: zapretSocketLimit, Flags: unix.BPF_F_RDONLY_PROG | unix.BPF_F_NO_PREALLOC})
	if err != nil {
		return nil, fmt.Errorf("create Zapret socket admission map: %w", err)
	}
	for i, family := range []struct {
		pin string
		num link.NetfilterProtocolFamily
		use bool
	}{{"ipv4", link.NetfilterProtoIPv4, scope.Egress().IPv4Gateway.IsValid()}, {"ipv6", link.NetfilterProtoIPv6, scope.Egress().IPv6Gateway.IsValid()}} {
		program, err := ebpf.NewProgram(&ebpf.ProgramSpec{Name: "sui_zapret_out", Type: ebpf.Netfilter, AttachType: ebpf.AttachNetfilter,
			License: "GPL", Instructions: zapretGuardProgram(plan, layout, g.device.FD(), g.deadline.FD(), g.sockets.FD(), raw, i, family.use)})
		if err != nil {
			return nil, fmt.Errorf("load Zapret %s egress guard: %w", family.pin, err)
		}
		hook, err := link.AttachNetfilter(link.NetfilterOptions{Program: program, ProtocolFamily: family.num, Hook: link.NetfilterInetPostRouting, Priority: zapretGuardPriority})
		_ = program.Close()
		if err != nil {
			return nil, fmt.Errorf("attach Zapret %s POSTROUTING guard: %w", family.pin, err)
		}
		info, err := hook.Info()
		if err != nil || info.ID == 0 || info.Program == 0 {
			_ = hook.Close()
			return nil, errors.Join(errors.New("observe new Zapret guard identity"), err)
		}
		g.hooks = append(g.hooks, zapretGuardPin{Link: hook, path: filepath.Join(pinDir, family.pin), id: info.ID, program: info.Program})
	}
	return g, nil
}

func (g *ZapretNativeGuard) pinLinks() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.revoked || g.pinned || len(g.hooks) != 2 {
		return errors.New("guard for Zapret cannot be pinned")
	}
	if err := checkZapretPinDirectory(filepath.Dir(g.hooks[0].path)); err != nil {
		return err
	}
	for i := range g.hooks {
		hook := &g.hooks[i]
		if err := hook.Pin(hook.path); err != nil {
			return fmt.Errorf("pin Zapret guard: %w", err)
		}
		hook.pinned = true
	}
	g.pinned = true
	return nil
}

func (g *ZapretNativeGuard) verifiedLinks() (ids [2]uint32, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.revoked || len(g.hooks) != len(ids) {
		return ids, errors.New("egress guard for Zapret is unavailable")
	}
	for i, hook := range g.hooks {
		current := hook.Link
		if g.pinned {
			var err error
			current, err = link.LoadPinnedLink(hook.path, nil)
			if err != nil {
				return ids, fmt.Errorf("load Zapret guard pin: %w", err)
			}
		}
		info, err := current.Info()
		if g.pinned {
			_ = current.Close()
		}
		if err != nil || info.ID != hook.id || info.Program != hook.program {
			return ids, errors.Join(errors.New("guard link or program for Zapret changed"), err)
		}
		ids[i] = uint32(info.ID)
	}
	return ids, nil
}

// ZapretGuardLinksGone proves the old kernel links do not exist in this
// boot. Missing pin names are not enough, and permission errors are not
// evidence of absence. This does not detach, adopt or change any link.
func ZapretGuardLinksGone(ids [2]uint32) bool {
	if ids[0] == 0 || ids[1] == 0 || ids[0] == ids[1] {
		return false
	}
	for _, id := range ids {
		hook, err := link.NewFromID(link.ID(id))
		if err == nil {
			_ = hook.Close()
			return false
		}
		if !errors.Is(err, unix.ENOENT) {
			return false
		}
	}
	return true
}

func (g *ZapretNativeGuard) leaseActive() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	var now unix.Timespec
	var deadline uint64
	return !g.closed && !g.revoked && g.pinned && g.deadline != nil && unix.ClockGettime(unix.CLOCK_MONOTONIC, &now) == nil &&
		g.deadline.Lookup(uint32(0), &deadline) == nil && deadline > uint64(now.Nano())
}

func checkZapretPinDirectory(path string) error {
	info, err := os.Lstat(path)
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("guard for Zapret requires a private bpffs generation directory")
	}
	var stat unix.Stat_t
	var fs unix.Statfs_t
	if err := unix.Lstat(path, &stat); err != nil || stat.Uid != 0 || unix.Statfs(path, &fs) != nil || fs.Type != unix.BPF_FS_MAGIC {
		return errors.New("guard directory for Zapret is not root-owned bpffs")
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		return errors.New("refusing to adopt a pre-existing Zapret guard generation")
	}
	return nil
}

// Renew extends a short kernel-monotonic lease after the owner re-verifies
// the generation. It cannot re-enroll a replaced device: only the initial
// setup populates the DEVMAP, and unregister removes the kernel reference.
func (g *ZapretNativeGuard) Renew(lifetime time.Duration) error {
	if g == nil || lifetime <= 0 || lifetime > 10*time.Second {
		return errors.New("invalid Zapret egress guard lease")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.revoked || !g.pinned || g.deadline == nil {
		return errors.New("egress guard for Zapret is not pinned and active")
	}
	var ifindex uint32
	if err := g.device.Lookup(uint32(0), &ifindex); err != nil || ifindex != g.ifindex {
		return errors.Join(errors.New("egress device for Zapret was removed or replaced"), g.revokeLocked())
	}
	var now unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &now); err != nil {
		return err
	}
	var previous uint64
	if err := g.deadline.Lookup(uint32(0), &previous); err != nil {
		return errors.Join(err, g.revokeLocked())
	}
	if previous != 0 && previous <= uint64(now.Nano()) {
		return errors.Join(errors.New("egress guard lease for Zapret expired"), g.revokeLocked())
	}
	g.leased = true // Raw output may be authorized even before socket enrollment.
	if err := g.deadline.Put(uint32(0), uint64(now.Nano()+int64(lifetime))); err != nil {
		return errors.Join(err, g.revokeLocked())
	}
	return nil
}

// EnrollSocket is for the authenticated adapter fence only. The broker must
// attest and validate the socket, enroll it and then mark it under that fence.
func (g *ZapretNativeGuard) EnrollSocket(fd int) error {
	if g == nil {
		return errors.New("egress guard for Zapret is unavailable")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.revoked || !g.pinned || g.sockets == nil {
		return errors.New("egress guard for Zapret is not pinned and active")
	}
	var now unix.Timespec
	var deadline uint64
	if unix.ClockGettime(unix.CLOCK_MONOTONIC, &now) != nil || g.deadline.Lookup(uint32(0), &deadline) != nil || deadline <= uint64(now.Nano()) {
		return errors.New("egress guard for Zapret has no live lease")
	}
	namespace, err := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	if err != nil || namespace != g.namespace {
		return errors.New("socket for Zapret is not in the guarded namespace")
	}
	cookie, err := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_COOKIE)
	if err != nil || cookie == 0 {
		return errors.New("socket for Zapret has no kernel cookie")
	}
	if err := g.sockets.Update(cookie, uint32(1), ebpf.UpdateNoExist); err != nil {
		return fmt.Errorf("enroll Zapret socket: %w", err)
	}
	return nil
}

func (g *ZapretNativeGuard) Revoke() error {
	if g == nil {
		return errors.New("egress guard for Zapret is unavailable")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.revokeLocked()
}

func (g *ZapretNativeGuard) revokeLocked() error {
	g.revoked = true
	if g.closed || g.deadline == nil {
		return errors.New("egress guard for Zapret is unavailable")
	}
	return g.deadline.Put(uint32(0), uint64(0))
}

func (g *ZapretNativeGuard) Close() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closeLocked()
}

// AbortUnleased rolls back a failed apply only before any lease could enable
// either raw output or socket admission. Later retirement keeps pins intact.
func (g *ZapretNativeGuard) AbortUnleased() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.leased {
		return errors.New("guard for Zapret may still protect leased traffic")
	}
	if g.closed {
		return errors.New("cannot abort a closed Zapret guard without live pin identity")
	}
	var err error
	if g.deadline != nil {
		err = g.revokeLocked()
	}
	for _, hook := range g.hooks {
		if !hook.pinned {
			continue
		}
		current, loadErr := link.LoadPinnedLink(hook.path, nil)
		if errors.Is(loadErr, os.ErrNotExist) {
			continue
		}
		if loadErr != nil {
			err = errors.Join(err, loadErr)
			continue
		}
		info, infoErr := current.Info()
		_ = current.Close()
		if infoErr != nil || info.ID != hook.id || info.Program != hook.program {
			err = errors.Join(err, errors.New("guard pin for Zapret changed ownership"), infoErr)
			continue
		}
		err = errors.Join(err, hook.Unpin())
	}
	return errors.Join(err, g.closeLocked())
}

func (g *ZapretNativeGuard) closeLocked() error {
	if g.closed {
		return nil
	}
	var err error
	if g.deadline != nil {
		err = errors.Join(err, g.revokeLocked())
	}
	g.closed = true
	for _, hook := range g.hooks {
		err = errors.Join(err, hook.Close())
	}
	if g.device != nil {
		err = errors.Join(err, g.device.Close())
	}
	if g.deadline != nil {
		err = errors.Join(err, g.deadline.Close())
	}
	if g.sockets != nil {
		err = errors.Join(err, g.sockets.Close())
	}
	return err
}

type zapretKernelLayout struct {
	state, skb, out, sk, ifindex, socketMark, socketCookie, packetMark int16
}

func observeZapretKernelLayout() (zapretKernelLayout, error) {
	spec, err := btf.LoadKernelSpec()
	if err != nil {
		return zapretKernelLayout{}, err
	}
	var layout zapretKernelLayout
	var sockCommon, sockCommonCookie int16
	for _, field := range []struct {
		structure, member string
		offset            *int16
	}{
		{"bpf_nf_ctx", "state", &layout.state}, {"bpf_nf_ctx", "skb", &layout.skb},
		{"nf_hook_state", "out", &layout.out}, {"nf_hook_state", "sk", &layout.sk},
		{"net_device", "ifindex", &layout.ifindex}, {"sock", "sk_mark", &layout.socketMark},
		{"sock", "__sk_common", &sockCommon}, {"sock_common", "skc_cookie", &sockCommonCookie},
		{"sk_buff", "mark", &layout.packetMark},
	} {
		var structure *btf.Struct
		if err := spec.TypeByName(field.structure, &structure); err != nil {
			return layout, err
		}
		bits, ok := zapretMemberOffset(structure, field.member)
		if !ok || bits%8 != 0 || bits.Bytes() > math.MaxInt16 {
			return layout, fmt.Errorf("missing kernel field %s.%s", field.structure, field.member)
		}
		*field.offset = int16(bits.Bytes())
	}
	if int(sockCommon)+int(sockCommonCookie) > math.MaxInt16 {
		return layout, errors.New("invalid kernel socket cookie offset")
	}
	layout.socketCookie = sockCommon + sockCommonCookie
	return layout, nil
}

func zapretMemberOffset(typ btf.Type, name string) (btf.Bits, bool) {
	var members []btf.Member
	switch typ := btf.UnderlyingType(typ).(type) {
	case *btf.Struct:
		members = typ.Members
	case *btf.Union:
		members = typ.Members
	}
	for _, member := range members {
		if member.Name == name && member.BitfieldSize == 0 {
			return member.Offset, true
		}
		if member.Name == "" {
			if off, ok := zapretMemberOffset(member.Type, name); ok {
				return member.Offset + off, true
			}
		}
	}
	return 0, false
}

func zapretGuardProgram(plan planner.ZapretPlan, layout zapretKernelLayout, device, deadline, sockets int, raw [2]uint64, family int, allowed bool) asm.Instructions {
	owned := int32(plan.QueueMark() | plan.LoopMark() | plan.FinalRouteBit())
	// Classify by both skb and original socket mark, so a later base chain
	// clearing a marked socket's skb mark cannot turn it into unrelated traffic.
	program := asm.Instructions{
		asm.LoadMem(asm.R6, asm.R1, layout.state, asm.DWord),
		asm.LoadMem(asm.R7, asm.R1, layout.skb, asm.DWord),
		asm.LoadMem(asm.R7, asm.R7, layout.packetMark, asm.Word),
		asm.LoadMem(asm.R9, asm.R6, layout.sk, asm.DWord),
	}
	if raw[0] != 0 || raw[1] != 0 {
		// A known generator socket is never ordinary traffic, even if a
		// later chain strips every Planner bit from its packet mark.
		program = append(program,
			asm.JEq.Imm(asm.R9, 0, "classify"),
			asm.LoadMem(asm.R0, asm.R9, layout.socketCookie, asm.DWord),
		)
		for i, cookie := range raw {
			if cookie == 0 {
				continue
			}
			verdict := "drop"
			if allowed && i == family {
				verdict = "generated"
			}
			program = append(program, asm.LoadImm(asm.R2, int64(cookie), asm.DWord), asm.JEq.Reg(asm.R0, asm.R2, verdict))
		}
	}
	program = append(program,
		asm.JSet.Imm(asm.R7, owned, "protected").WithSymbol("classify"),
		asm.JEq.Imm(asm.R9, 0, "accept"),
		asm.LoadMem(asm.R2, asm.R9, layout.socketMark, asm.Word),
		asm.JSet.Imm(asm.R2, owned, "protected"),
		asm.Ja.Label("accept"),
	)
	if allowed {
		program = append(program,
			asm.JNE.Imm(asm.R7, int32(plan.QueueMark()|plan.FinalRouteBit()), "drop").WithSymbol("protected"),
			asm.JEq.Imm(asm.R9, 0, "drop"),
			asm.LoadMem(asm.R0, asm.R9, layout.socketMark, asm.Word),
			asm.JNE.Imm(asm.R0, int32(plan.SocketMark()), "drop"),
			asm.LoadMem(asm.R0, asm.R9, layout.socketCookie, asm.DWord),
			asm.StoreMem(asm.R10, -16, asm.R0, asm.DWord),
			asm.LoadMapPtr(asm.R1, sockets), asm.Mov.Reg(asm.R2, asm.R10), asm.Add.Imm(asm.R2, -16), asm.FnMapLookupElem.Call(),
			asm.JEq.Imm(asm.R0, 0, "drop"),
		)
		if raw[family] != 0 {
			program = append(program, asm.Ja.Label("path"),
				asm.JNE.Imm(asm.R7, int32(plan.RawOutputMark()|plan.FinalRouteBit()), "drop").WithSymbol("generated"))
		}
		program = append(program,
			asm.LoadMem(asm.R8, asm.R6, layout.out, asm.DWord).WithSymbol("path"),
			asm.JEq.Imm(asm.R8, 0, "drop"),
			asm.LoadMem(asm.R8, asm.R8, layout.ifindex, asm.Word),
			asm.StoreImm(asm.R10, -4, 0, asm.Word),
			asm.LoadMapPtr(asm.R1, device), asm.Mov.Reg(asm.R2, asm.R10), asm.Add.Imm(asm.R2, -4), asm.FnMapLookupElem.Call(),
			asm.JEq.Imm(asm.R0, 0, "drop"),
			asm.LoadMem(asm.R0, asm.R0, 0, asm.Word),
			asm.JNE.Reg(asm.R8, asm.R0, "drop"),
			asm.LoadMapPtr(asm.R1, deadline), asm.Mov.Reg(asm.R2, asm.R10), asm.Add.Imm(asm.R2, -4), asm.FnMapLookupElem.Call(),
			asm.JEq.Imm(asm.R0, 0, "drop"),
			asm.LoadMem(asm.R8, asm.R0, 0, asm.DWord), asm.FnKtimeGetNs.Call(),
			asm.JGE.Reg(asm.R0, asm.R8, "drop"), asm.Ja.Label("accept"),
		)
	} else {
		program = append(program, asm.Ja.Label("drop").WithSymbol("protected"))
	}
	return append(program, asm.Mov.Imm(asm.R0, 0).WithSymbol("drop"), asm.Return(), asm.Mov.Imm(asm.R0, 1).WithSymbol("accept"), asm.Return())
}
