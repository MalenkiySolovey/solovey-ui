package nftables

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/btf"
	"github.com/cilium/ebpf/link"
	"golang.org/x/sys/unix"
)

const (
	zapretPostrouting   = 4
	zapretGuardPriority = math.MaxInt32 - 1
)

// ZapretNativeGuard is owned by the installed Planner adapter. It starts
// closed. Pins are kernel lifetime anchors, not authority to adopt an old
// generation. Close deliberately leaves them attached.
type ZapretNativeGuard struct {
	device, deadline *ebpf.Map
	hooks            []link.Link
	ifindex          uint32
}

// CreateZapretNativeGuard requires an empty, root-owned generation directory
// on bpffs. No marked socket may be issued until both hooks are pinned and
// the queue/rules have been verified and committed by the generation owner.
func CreateZapretNativeGuard(plan planner.ZapretPlan, pinDir string) (_ *ZapretNativeGuard, failure error) {
	scope, err := plan.SocketScope()
	if err != nil || plan.RawPassMark() == 0 {
		return nil, errors.New("Zapret guard requires an installed Planner allocation")
	}
	if err := checkZapretPinDirectory(pinDir); err != nil {
		return nil, err
	}
	if strconv.IntSize != 64 {
		return nil, errors.New("Zapret BPF guard requires a 64-bit Linux kernel and broker")
	}
	layout, err := observeZapretKernelLayout()
	if err != nil {
		return nil, fmt.Errorf("Zapret egress guard requires kernel BTF: %w", err)
	}
	g := &ZapretNativeGuard{ifindex: uint32(scope.Egress().IfIndex)}
	var pins []string
	defer func() {
		if failure != nil {
			for _, pin := range pins {
				failure = errors.Join(failure, os.Remove(pin))
			}
			failure = errors.Join(failure, g.Close())
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
	for _, family := range []struct {
		pin string
		num uint32
		use bool
	}{{"ipv4", unix.NFPROTO_IPV4, scope.Egress().IPv4}, {"ipv6", unix.NFPROTO_IPV6, scope.Egress().IPv6}} {
		program, err := ebpf.NewProgram(&ebpf.ProgramSpec{Name: "sui_zapret_out", Type: ebpf.Netfilter, AttachType: ebpf.AttachNetfilter,
			License: "GPL", Instructions: zapretGuardProgram(plan, layout, g.device.FD(), g.deadline.FD(), family.use)})
		if err != nil {
			return nil, fmt.Errorf("load Zapret %s egress guard: %w", family.pin, err)
		}
		hook, err := link.AttachNetfilter(link.NetfilterOptions{Program: program, ProtocolFamily: family.num, HookNumber: zapretPostrouting, Priority: zapretGuardPriority})
		_ = program.Close()
		if err != nil {
			return nil, fmt.Errorf("attach Zapret %s POSTROUTING guard: %w", family.pin, err)
		}
		g.hooks = append(g.hooks, hook)
		pin := filepath.Join(pinDir, family.pin)
		if err := hook.Pin(pin); err != nil {
			return nil, fmt.Errorf("pin Zapret guard: %w", err)
		}
		pins = append(pins, pin)
	}
	return g, nil
}

func checkZapretPinDirectory(path string) error {
	info, err := os.Lstat(path)
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("Zapret guard requires a private bpffs generation directory")
	}
	var stat unix.Stat_t
	var fs unix.Statfs_t
	if err := unix.Lstat(path, &stat); err != nil || stat.Uid != 0 || unix.Statfs(path, &fs) != nil || fs.Type != unix.BPF_FS_MAGIC {
		return errors.New("Zapret guard generation directory is not root-owned bpffs")
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
	if g == nil || g.deadline == nil || lifetime <= 0 || lifetime > 10*time.Second {
		return errors.New("invalid Zapret egress guard lease")
	}
	var ifindex uint32
	if err := g.device.Lookup(uint32(0), &ifindex); err != nil || ifindex != g.ifindex {
		return errors.Join(errors.New("Zapret egress device was removed or replaced"), g.Revoke())
	}
	var now unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &now); err != nil {
		return err
	}
	return g.deadline.Put(uint32(0), uint64(now.Nano()+int64(lifetime)))
}

func (g *ZapretNativeGuard) Revoke() error {
	if g == nil || g.deadline == nil {
		return errors.New("Zapret egress guard is unavailable")
	}
	return g.deadline.Put(uint32(0), uint64(0))
}

func (g *ZapretNativeGuard) Close() error {
	if g == nil {
		return nil
	}
	var err error
	for _, hook := range g.hooks {
		err = errors.Join(err, hook.Close())
	}
	if g.device != nil {
		err = errors.Join(err, g.device.Close())
	}
	if g.deadline != nil {
		err = errors.Join(err, g.deadline.Close())
	}
	return err
}

type zapretKernelLayout struct {
	state, skb, out, sk, ifindex, socketMark, packetMark int16
}

func observeZapretKernelLayout() (zapretKernelLayout, error) {
	spec, err := btf.LoadKernelSpec()
	if err != nil {
		return zapretKernelLayout{}, err
	}
	var layout zapretKernelLayout
	for _, field := range []struct {
		structure, member string
		offset            *int16
	}{
		{"bpf_nf_ctx", "state", &layout.state}, {"bpf_nf_ctx", "skb", &layout.skb},
		{"nf_hook_state", "out", &layout.out}, {"nf_hook_state", "sk", &layout.sk},
		{"net_device", "ifindex", &layout.ifindex}, {"sock", "sk_mark", &layout.socketMark},
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

func zapretGuardProgram(plan planner.ZapretPlan, layout zapretKernelLayout, device, deadline int, family bool) asm.Instructions {
	owned := int32(plan.QueueMark() | plan.LoopMark() | plan.RawPassMark())
	// Classify by both skb and original socket mark, so a later base chain
	// clearing a marked socket's skb mark cannot turn it into unrelated traffic.
	program := asm.Instructions{
		asm.LoadMem(asm.R6, asm.R1, layout.state, asm.DWord),
		asm.LoadMem(asm.R7, asm.R1, layout.skb, asm.DWord),
		asm.LoadMem(asm.R7, asm.R7, layout.packetMark, asm.Word),
		asm.JSet.Imm(asm.R7, owned, "protected"),
		asm.LoadMem(asm.R2, asm.R6, layout.sk, asm.DWord),
		asm.JEq.Imm(asm.R2, 0, "accept"),
		asm.LoadMem(asm.R2, asm.R2, layout.socketMark, asm.Word),
		asm.JSet.Imm(asm.R2, owned, "protected"),
		asm.Ja.Label("accept"),
	}
	if family {
		program = append(program,
			asm.JEq.Imm(asm.R7, int32(plan.QueueMark()), "path").WithSymbol("protected"),
			asm.JEq.Imm(asm.R7, int32(plan.LoopMark()|plan.RawPassMark()), "path"),
			asm.JNE.Imm(asm.R7, int32(plan.LoopMark()|plan.QueueMark()|plan.RawPassMark()), "drop"),
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
