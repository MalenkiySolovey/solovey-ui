//go:build linux && !minimal

package privilegedbroker

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/link"
	"golang.org/x/sys/unix"
)

// This is a capability and hook-order probe, not an installed guard. Here
// the test itself applies a synthetic pass bit without an NFQUEUE consumer.
func TestSystemdZapretPacketHookCandidates(t *testing.T) {
	pins := os.Getenv("SOLOVEY_TEST_ZAPRET_PACKET_HOOKS")
	if pins == "" {
		t.Skip("requires the disposable systemd packet-hook runner")
	}
	insistZapretServiceIsolation(t, 0, "00000080000830db")
	dev, err := net.InterfaceByName("zproof0")
	if err != nil || dev.Flags&net.FlagUp == 0 {
		t.Fatalf("disposable redirect fixture is not up: %v", err)
	}
	group, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	groupName, ok := strings.CutPrefix(strings.TrimSpace(string(group)), "0::/system.slice/")
	if !ok || !strings.HasPrefix(groupName, "solovey-zsocket-") || !strings.HasSuffix(groupName, "-packet-hooks.service") || filepath.Base(groupName) != groupName {
		t.Fatal("refusing to attach outside this test's dedicated systemd cgroup")
	}
	const socketMark, queueMark = 0x80, 0x180
	const skbMark, skbSock, sockMark = 8, 168, 16 // Linux BPF UAPI offsets, not kernel struct layout.
	open := func(mark int) int {
		t.Helper()
		fd := openZapretServiceSocket(t, unix.SOCK_DGRAM)
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_MARK, mark); err != nil {
			t.Fatal(err)
		}
		return fd
	}
	a, b, unknown := open(queueMark), open(0), open(queueMark)
	cookie, err := unix.GetsockoptUint64(a, unix.SOL_SOCKET, unix.SO_COOKIE)
	if err != nil || cookie == 0 {
		t.Fatalf("no kernel socket cookie: %v", err)
	}
	load := func(typ ebpf.ProgramType, att ebpf.AttachType, code asm.Instructions) *ebpf.Program {
		t.Helper()
		prog, err := ebpf.NewProgram(&ebpf.ProgramSpec{Type: typ, AttachType: att, License: "GPL", Instructions: code})
		if err != nil {
			t.Fatalf("load %s under scoped broker: %+v", att, err)
		}
		t.Cleanup(func() { _ = prog.Close() })
		return prog
	}
	retain := func(hook link.Link, err error, name string) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = hook.Close() })
		if err := hook.Pin(filepath.Join(pins, name)); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := hook.Unpin(); err != nil {
				t.Error(err)
			}
		})
	}
	routes := func() (pair [2]uint64) {
		t.Helper()
		for i, device := range []string{"zproof1", "zprobe3"} {
			out, err := exec.CommandContext(t.Context(), "ip", "-j", "-s", "link", "show", "dev", device).Output()
			if err != nil {
				t.Fatal(err)
			}
			var rows []struct {
				Stats *struct{ Rx struct{ Packets uint64 } } `json:"stats64"`
			}
			if err := json.Unmarshal(out, &rows); err != nil || len(rows) != 1 || rows[0].Stats == nil {
				t.Fatalf("unavailable veth receive counters for %s: %v", device, err)
			}
			pair[i] = rows[0].Stats.Rx.Packets
		}
		return pair
	}
	check := func(fd int, label string, want [2]uint64) {
		t.Helper()
		before := routes()
		dest := &unix.SockaddrInet4{Addr: [4]byte{198, 51, 100, 25}, Port: 4443}
		sendErr := unix.Sendto(fd, []byte(label), 0, dest)
		if sendErr != nil && want != [2]uint64{} {
			t.Fatalf("%s: %v", label, sendErr)
		}
		until := time.Now().Add(180 * time.Millisecond)
		for {
			now := routes()
			got := [2]uint64{now[0] - before[0], now[1] - before[1]}
			if (want != [2]uint64{} && got == want) || time.Now().After(until) {
				if got != want {
					t.Fatalf("%s: received %v, want %v (send: %v)", label, got, want, sendErr)
				}
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	check(a, "unguarded A redirects", [2]uint64{0, 1})
	cg := load(ebpf.CGroupSKB, ebpf.AttachCGroupInetEgress, asm.Instructions{
		asm.LoadMem(asm.R2, asm.R1, skbMark, asm.Word),
		asm.JEq.Imm(asm.R2, queueMark, "cookie"),
		asm.LoadMem(asm.R1, asm.R1, skbSock, asm.DWord),
		asm.JEq.Imm(asm.R1, 0, "allow"),
		asm.FnSkFullsock.Call(),
		asm.JEq.Imm(asm.R0, 0, "allow"),
		asm.LoadMem(asm.R2, asm.R0, sockMark, asm.Word),
		asm.JSet.Imm(asm.R2, socketMark, "deny"),
		asm.Ja.Label("allow"),
		asm.FnGetSocketCookie.Call().WithSymbol("cookie"),
		asm.LoadImm(asm.R2, int64(cookie), asm.DWord),
		asm.JEq.Reg(asm.R0, asm.R2, "allow"),
		asm.Mov.Imm(asm.R0, 0).WithSymbol("deny"), asm.Return(),
		asm.Mov.Imm(asm.R0, 1).WithSymbol("allow"), asm.Return(),
	})
	cgroupHook, err := link.AttachCgroup(link.CgroupOptions{Path: filepath.Join("/sys/fs/cgroup/system.slice", groupName), Attach: ebpf.AttachCGroupInetEgress, Program: cg})
	retain(cgroupHook, err, "test_cgroup")
	check(unknown, "unadmitted socket cookie drops", [2]uint64{})
	check(open(socketMark), "socket without queue pass drops", [2]uint64{})
	check(a, "admitted cookie still redirects without TCX", [2]uint64{0, 1})
	t.Log("SCOPED_CGROUP_SKB=PASS (socket mark, cookie, pin and packet verdicts)")
	tcx := load(ebpf.SchedCLS, ebpf.AttachTCXEgress, asm.Instructions{
		asm.LoadMem(asm.R2, asm.R1, skbMark, asm.Word),
		asm.JEq.Imm(asm.R2, queueMark, "pass"),
		asm.Mov.Imm(asm.R0, -1), asm.Return(), // TCX_NEXT: preserve unrelated classifiers.
		asm.Mov.Imm(asm.R0, 0).WithSymbol("pass"), asm.Return(), // TCX_PASS: skip later classifiers.
	})
	query := func() *link.QueryResult {
		t.Helper()
		result, err := link.QueryPrograms(link.QueryOptions{Target: dev.Index, Attach: ebpf.AttachTCXEgress})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := query()
	if len(before.Programs) != 0 {
		t.Fatal("unexpected existing TCX egress program in the disposable fixture")
	}
	tcxHook, err := link.AttachTCX(link.TCXOptions{Interface: dev.Index, Program: tcx, Attach: ebpf.AttachTCXEgress,
		Anchor: link.Head(), ExpectedRevision: before.Revision})
	retain(tcxHook, err, "test_tcx")
	info, err := tcxHook.Info()
	if err != nil {
		t.Fatal(err)
	}
	first := query()
	if len(first.Programs) != 1 || first.Revision == 0 {
		t.Fatal("cannot establish the initial TCX link order and revision")
	}
	if id, ok := first.Programs[0].LinkID(); !ok || id != info.ID {
		t.Fatal("verified TCX egress program is not the owned link")
	}
	check(a, "TCX stops late redirect for A", [2]uint64{1, 0})
	check(b, "foreign late redirect unchanged for B", [2]uint64{0, 1})
	t.Log("SCOPED_TCX=PASS; LATE_REDIRECT_A=PREVENTED; UNMARKED_REDIRECT=UNCHANGED")

	// Attachment revision is compare-at-attach, not a reservation. This
	// same-namespace fixture changes the chain after A has been admitted.
	// Do not claim a netdev gate from link identity alone.
	alternate, err := net.InterfaceByName("zprobe2")
	if err != nil {
		t.Fatal(err)
	}
	redirect := load(ebpf.SchedCLS, ebpf.AttachTCXEgress, asm.Instructions{
		asm.LoadMem(asm.R2, asm.R1, skbMark, asm.Word),
		asm.JEq.Imm(asm.R2, queueMark, "redirect"),
		asm.Mov.Imm(asm.R0, -1), asm.Return(),
		asm.Mov.Imm(asm.R1, int32(alternate.Index)).WithSymbol("redirect"),
		asm.Mov.Imm(asm.R2, 0), asm.FnRedirect.Call(), asm.Return(),
	})
	competing, err := link.AttachTCX(link.TCXOptions{Interface: dev.Index, Program: redirect, Attach: ebpf.AttachTCXEgress,
		Anchor: link.Head(), ExpectedRevision: first.Revision})
	if err != nil {
		t.Fatal(err)
	}
	defer competing.Close()
	changed := query()
	if len(changed.Programs) != 2 || changed.Revision == first.Revision {
		t.Fatal("the expected TCX order change was not visible to userspace")
	}
	if id, ok := changed.Programs[1].LinkID(); !ok || id != info.ID {
		t.Fatal("the test replaced the guard instead of inserting ahead of it")
	}
	check(a, "earlier TCX wins despite retained guard", [2]uint64{0, 1})
	if err := competing.Close(); err != nil {
		t.Fatal(err)
	}
	check(a, "admitted A returns to the expected egress", [2]uint64{1, 0})
	t.Log("TCX_EGRESS_ORDER_MUTABLE=OBSERVED; PINNED_LINK_STILL_PRESENT=PASS; SCOPED_TCX_FINAL_GATE=NOT_PROVEN")
}
