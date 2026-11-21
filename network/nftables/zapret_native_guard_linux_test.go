package nftables

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/network/intent"
	platform "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"github.com/cilium/ebpf"
	"golang.org/x/sys/unix"
)

func TestZapretNativeGuardRoutedEgress(t *testing.T) {
	pins := os.Getenv("SOLOVEY_TEST_GUARD_ROOT")
	engine := os.Getenv("SOLOVEY_TEST_ENGINE_ROOT")
	parent := os.Getenv("SOLOVEY_TEST_PARENT_NETNS")
	if pins == "" || engine == "" || parent == "" {
		t.Skip("requires an isolated namespace and the source-pinned engine")
	}
	if _, err := probeNamespaceForGuard(parent); err != nil {
		t.Fatal(err)
	}
	const dst, port = "198.51.100.1", 4443
	const foreign = "add table inet r8b1_foreign\nadd chain inet r8b1_foreign output { type filter hook output priority 0; policy accept; }\n"
	run := func(input, tool string, args ...string) string {
		t.Helper()
		command := exec.CommandContext(t.Context(), tool, args...)
		command.Stdin = strings.NewReader(input)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", tool, args, err, out)
		}
		return string(out)
	}
	ip := func(args ...string) { t.Helper(); run("", "ip", args...) }
	nft := func(input string) { t.Helper(); run(input, "nft", "-f", "-") }
	route := func(device string) { t.Helper(); ip("-4", "route", "replace", dst+"/32", "dev", device) }
	link := func(device, peer, addr string, index int) {
		t.Helper()
		args := []string{"link", "add", "name", device}
		if index != 0 {
			args = append(args, "index", strconv.Itoa(index))
		}
		ip(append(args, "type", "veth", "peer", "name", peer)...)
		ip("-4", "addr", "add", addr+"/32", "dev", device)
		ip("link", "set", device, "up")
		ip("link", "set", peer, "up")
		linkPeer, err := net.InterfaceByName(peer)
		if err != nil {
			t.Fatal(err)
		}
		ip("-4", "neigh", "replace", dst, "lladdr", linkPeer.HardwareAddr.String(), "nud", "permanent", "dev", device)
	}
	link("r8ba", "r8bap", "192.0.2.2", 0)
	link("r8bb", "r8bbp", "192.0.2.3", 0)
	route("r8ba")
	iface, err := net.InterfaceByName("r8ba")
	if err != nil {
		t.Fatal(err)
	}
	a := testGuardUDPSocket(t)
	b := testGuardUDPSocket(t)
	plan := testGuardPlan(t, a, iface.Index, port)
	guard, err := CreateZapretNativeGuard(plan, pins)
	if err != nil {
		var verifier *ebpf.VerifierError
		if errors.As(err, &verifier) {
			t.Logf("kernel verifier: %+v", verifier)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	if err := unix.SetsockoptInt(a, unix.SOL_SOCKET, unix.SO_MARK, int(plan.SocketMark())); err != nil {
		t.Fatal(err)
	}
	var engineLog bytes.Buffer
	proc := exec.CommandContext(t.Context(), filepath.Join(engine, "nfqws2"), fmt.Sprintf("--qnum=%d", plan.Queue()), "--filter-udp=*")
	proc.Stdout, proc.Stderr = &engineLog, &engineLog
	if err := proc.Start(); err != nil {
		t.Fatal(err)
	}
	stopEngine := syncProcessCleanup(t, proc, &engineLog)
	buffer := filepath.Join("/proc/net/netfilter", "nfnetlink_queue")
	queueReady := false
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		data, err := os.ReadFile(buffer)
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			fields := strings.Fields(line)
			queueReady = queueReady || len(fields) >= 4 && fields[0] == strconv.Itoa(int(plan.Queue())) && fields[3] == "2"
		}
		if queueReady {
			break
		}
	}
	if !queueReady {
		t.Fatal("reproducibly built NFQUEUE consumer never became ready")
	}
	rules, err := RenderInstalledZapret(plan)
	if err != nil {
		t.Fatal(err)
	}
	nft(foreign)
	foreignBefore := run("", "nft", "list", "table", "inet", "r8b1_foreign")
	nft(rules)
	peer := testGuardReceiver(t, "r8bap")
	check := func(sock, receiver int, title string, want bool) {
		t.Helper()
		marker := []byte("r8b1:" + title)
		err := unix.Sendto(sock, marker, unix.MSG_DONTWAIT, &unix.SockaddrInet4{Port: port, Addr: [4]byte{198, 51, 100, 1}})
		if err != nil && want {
			t.Fatalf("send %s: %v", title, err)
		}
		if seen := testGuardPacket(t, receiver, marker); seen != want {
			t.Fatalf("%s seen on wire=%t, wanted %t (send result: %v)", title, seen, want, err)
		}
	}
	check(a, peer, "uncommitted", false)
	if err := guard.Renew(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	check(a, peer, "active A", true)
	check(b, peer, "active B", true)
	remove := "delete table inet " + planner.ZapretInstalledTable + "\n"
	nft(remove)
	check(a, peer, "table gone", false)
	check(b, peer, "table gone B", true)
	nft(rules)
	alternate := testGuardReceiver(t, "r8bbp")
	route("r8bb")
	check(a, alternate, "alternate route A", false)
	check(b, alternate, "alternate route B", true)
	route("r8ba")
	check(a, peer, "restored egress", true)
	stopEngine()
	check(a, peer, "lost consumer", false)
	check(b, peer, "lost consumer B", true)
	// Model packet permission without the nft table, to directly challenge
	// the kernel reference rather than just testing nft's oif comparison.
	nft(remove)
	raw := testGuardUDPSocket(t)
	if err := unix.SetsockoptInt(raw, unix.SOL_SOCKET, unix.SO_MARK, int(plan.LoopMark()|plan.QueueMark())); err != nil {
		t.Fatal(err)
	}
	check(raw, peer, "generated packet with inherited queue mark", false)
	if err := unix.SetsockoptInt(raw, unix.SOL_SOCKET, unix.SO_MARK, int(plan.LoopMark()|plan.RawPassMark())); err != nil {
		t.Fatal(err)
	}
	check(raw, peer, "control with permission", true)
	ip("link", "del", "r8ba")
	link("r8ba", "r8bap", "192.0.2.2", iface.Index)
	route("r8ba")
	replaced := testGuardReceiver(t, "r8bap")
	check(raw, replaced, "recreated ifindex with an otherwise valid mark", false)
	check(b, replaced, "recreated ifindex B", true)
	if err := guard.Renew(time.Second); err == nil {
		t.Fatal("replaced device was admitted into the old generation")
	}
	// Closing these FDs simulates owner loss while the pins and marked
	// sockets survive. The independent hook must remain attached.
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	check(a, replaced, "old marked socket after owner close", false)
	check(b, replaced, "unmarked after owner close", true)
	if after := run("", "nft", "list", "table", "inet", "r8b1_foreign"); after != foreignBefore {
		t.Fatal("pre-existing firewall was changed")
	}
	t.Log("veth wire: A delivered only while admitted; B unchanged; table/queue/route loss, generated-mark inheritance, ifindex reuse and owner loss denied")
}

func probeNamespaceForGuard(parent string) (string, error) {
	ns, err := os.Readlink("/proc/self/ns/net")
	initNS, initErr := os.Readlink("/proc/1/ns/net")
	links, linkErr := net.Interfaces()
	if !strings.HasPrefix(parent, "net:[") || err != nil || initErr != nil || ns == parent || ns == initNS || os.Geteuid() != 0 ||
		linkErr != nil || len(links) != 1 || links[0].Name != "lo" {
		return "", errors.New("Zapret egress test requires a fresh, disposable network namespace")
	}
	return ns, nil
}

func syncProcessCleanup(t *testing.T, proc *exec.Cmd, logs *bytes.Buffer) func() {
	var stopped bool
	stop := func() {
		if !stopped {
			stopped = true
			_ = proc.Process.Kill()
			_ = proc.Wait()
		}
	}
	t.Cleanup(func() {
		stop()
		if t.Failed() {
			t.Log(logs.String())
		}
	})
	return stop
}

func testGuardUDPSocket(t *testing.T) int {
	t.Helper()
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, unix.IPPROTO_UDP)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return fd
}

func testGuardReceiver(t *testing.T, name string) int {
	t.Helper()
	iface, err := net.InterfaceByName(name)
	if err != nil {
		t.Fatal(err)
	}
	protocol := binary.NativeEndian.Uint16([]byte{0x08, 0x00}) // ETH_P_IP, network byte order
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, int(protocol))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	if err := unix.Bind(fd, &unix.SockaddrLinklayer{Ifindex: iface.Index, Protocol: protocol}); err != nil {
		t.Fatal(err)
	}
	return fd
}

func testGuardPacket(t *testing.T, fd int, marker []byte) bool {
	t.Helper()
	end := time.Now().Add(220 * time.Millisecond)
	var packet [2048]byte
	for time.Now().Before(end) {
		if _, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, int(time.Until(end).Milliseconds())+1); err != nil {
			t.Fatal(err)
		}
		n, _, err := unix.Recvfrom(fd, packet[:], unix.MSG_DONTWAIT)
		if err == nil && bytes.HasSuffix(packet[:n], marker) {
			return true
		}
		if err != nil && !errors.Is(err, unix.EAGAIN) {
			t.Fatal(err)
		}
	}
	return false
}

func testGuardPlan(t *testing.T, fd, ifindex int, port uint16) planner.ZapretPlan {
	t.Helper()
	var cookie uint64
	length := uint32(8)
	_, _, errno := unix.Syscall6(unix.SYS_GETSOCKOPT, uintptr(fd), unix.SOL_SOCKET, unix.SO_NETNS_COOKIE,
		uintptr(unsafe.Pointer(&cookie)), uintptr(unsafe.Pointer(&length)), 0)
	if errno != 0 || length != 8 || cookie == 0 {
		t.Fatalf("observe fixture socket namespace: %v", errno)
	}
	revision := strings.Repeat("a", 64)
	plan, err := planner.CompileZapret(intent.ZapretPacketPathIntent{
		Schema: intent.ZapretPacketPathSchema, HostRef: hostref.Local(), Owner: intent.ZapretOwner,
		ResourceRef: "outbound:direct-a", OutboundRef: "direct-a", StrategyRef: "guard-test", Purpose: intent.ZapretPurpose,
		StrategyRevision: revision, InputRevision: revision, DesiredRevision: revision,
		Protocols: []intent.PacketProtocol{intent.PacketProtocolUDP}, UDPPorts: []uint16{port},
		Directions: []intent.PacketDirection{intent.PacketDirectionOutbound},
	}, planner.Facts{
		HostRef: hostref.Local(), Revision: revision, CapabilityRevision: revision, InventoryComplete: true,
		Capability: platform.PlatformFact{Availability: platform.Available, Backend: planner.ZapretInstalledBackend},
		Zapret: &planner.ZapretFacts{
			OutboundRef: "direct-a", OutboundRevision: revision, Provider: "sing-box",
			DirectSocket: true, SocketMark: true, NFQueue: true, ConntrackAccounting: true, EgressGuard: true,
			InstalledEgress: &planner.ZapretEgress{NamespaceCookie: cookie, IfIndex: int32(ifindex), IPv4: true, RouteRevision: revision},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
