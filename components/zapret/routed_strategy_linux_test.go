//go:build linux && zapretproof && !minimal

package zapret

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-component-zapret/binding"
	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/network/intent"
	hostoutbounds "github.com/MalenkiySolovey/solovey-host/outbounds"
	platform "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"golang.org/x/sys/unix"
)

// TestZapretRoutedStrategyWire exercises the real engine and guard, not the
// installed Panel or privileged broker lifecycle. Socket admission below is
// fixture-owned; the only destination is another disposable namespace.
func TestZapretRoutedStrategyWire(t *testing.T) {
	parent, engine, pins := os.Getenv("SOLOVEY_TEST_PARENT_NETNS"), os.Getenv("SOLOVEY_TEST_ENGINE_ROOT"), os.Getenv("SOLOVEY_TEST_GUARD_ROOT")
	current, currentErr := os.Readlink("/proc/self/ns/net")
	initial, initialErr := os.Readlink("/proc/1/ns/net")
	interfaces, ifaceErr := net.Interfaces()
	if !strings.HasPrefix(parent, "net:[") || engine == "" || pins == "" || currentErr != nil || initialErr != nil ||
		current == parent || current == initial || os.Geteuid() != 0 || ifaceErr != nil || len(interfaces) != 1 || interfaces[0].Name != "lo" {
		t.Fatal("refusing routed strategy qualification outside a new disposable namespace")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	peer := exec.CommandContext(ctx, "unshare", "--net", "--", "sleep", "30")
	if err := peer.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = peer.Process.Kill(); _ = peer.Wait() }()
	pid := strconv.Itoa(peer.Process.Pid)
	peerNS := "/proc/" + pid + "/ns/net"
	for deadline := time.Now().Add(time.Second); ; time.Sleep(5 * time.Millisecond) {
		if target, err := os.Readlink(peerNS); err == nil && target != current && target != initial && target != parent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("receiver network namespace did not start")
		}
	}
	run := func(input, executable string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Stdin = strings.NewReader(input)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v: %v\n%s", executable, args, err, out)
		}
	}
	const tx, rx = "r8b1tx", "r8b1rx"
	const senderIP, receiverIP = "192.0.2.1", "192.0.2.2"
	run("", "ip", "link", "add", tx, "type", "veth", "peer", "name", rx)
	run("", "ip", "link", "set", rx, "netns", pid)
	run("", "ip", "addr", "add", senderIP+"/30", "dev", tx)
	run("", "ip", "link", "set", tx, "up")
	run("", "nsenter", "-t", pid, "-n", "--", "ip", "addr", "add", receiverIP+"/30", "dev", rx)
	run("", "nsenter", "-t", pid, "-n", "--", "ip", "link", "set", rx, "up")
	run("", "nsenter", "-t", pid, "-n", "--", "ethtool", "-K", rx, "gro", "off")
	dev, err := net.InterfaceByName(tx)
	if err != nil {
		t.Fatal(err)
	}

	var listener *net.TCPListener
	var wire int
	inZapretReceiverNetNS(t, peerNS, func() {
		listener, err = net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP(receiverIP)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		iface, err := net.InterfaceByName(rx)
		if err != nil {
			t.Fatal(err)
		}
		proto := binary.NativeEndian.Uint16([]byte{0x08, 0x00}) // ETH_P_IP in network order
		wire, err = unix.Socket(unix.AF_PACKET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, int(proto))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = unix.Close(wire) })
		if err := unix.Bind(wire, &unix.SockaddrLinklayer{Ifindex: iface.Index, Protocol: proto}); err != nil {
			t.Fatal(err)
		}
	})
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	profile, err := strategy.Seal(strategy.Strategy{Schema: strategy.Schema, ID: "wire-http", Assets: []strategy.AssetRef{strategy.Library, strategy.Actions},
		Profiles: []strategy.Profile{{ID: "split", Protocol: intent.PacketProtocolTCP, Ports: []uint16{port}, Payload: strategy.HTTPRequest, SplitAt: 8}}})
	if err != nil {
		t.Fatal(err)
	}
	revision := strings.Repeat("a", 64)
	resolved, err := binding.Resolve(hostoutbounds.ZapretBinding{OutboundRef: "direct-a", StrategyRef: profile.ID, Enabled: true},
		binding.DirectOutbound{Host: hostref.Local(), Ref: "direct-a", Type: "direct", Revision: revision}, zapretFixtureSource{profile}, true)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	_ = unix.Close(fd)
	if err != nil {
		t.Fatal(err)
	}
	facts := planner.Facts{
		HostRef: hostref.Local(), Revision: revision, CapabilityRevision: revision, InventoryComplete: true,
		Capability: platform.PlatformFact{Availability: platform.Available, Backend: planner.ZapretInstalledBackend},
		Zapret: &planner.ZapretFacts{OutboundRef: resolved.Intent.OutboundRef, OutboundRevision: revision, Provider: "sing-box",
			DirectSocket: true, SocketMark: true, NFQueue: true, ConntrackAccounting: true, EgressGuard: true,
			InstalledEgress: &planner.ZapretEgress{NamespaceCookie: cookie, IfIndex: int32(dev.Index), IPv4: true, RouteRevision: revision}},
	}
	plan, err := planner.CompileZapret(resolved.Intent, facts)
	if err != nil {
		t.Fatal(err)
	}
	argv, err := resolved.Strategy.Compile(engine, plan.Queue(), plan.QueueMark(), plan.LoopMark())
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	consumer := exec.CommandContext(ctx, filepath.Join(engine, "nfqws2"), argv...)
	consumer.Stdout, consumer.Stderr = &log, &log
	if err := consumer.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = consumer.Process.Kill()
		_ = consumer.Wait()
		if t.Failed() {
			t.Log(log.String())
		}
	}()
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		listing, err := os.ReadFile("/proc/net/netfilter/nfnetlink_queue")
		fields := strings.Fields(string(listing))
		if err == nil && len(fields) == 9 && fields[0] == strconv.Itoa(int(plan.Queue())) && fields[3] == "2" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pinned engine did not bind the isolated queue")
		}
	}
	identity, err := processevidence.Observe(consumer.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	path, err := nftables.PrepareZapretNativePath(ctx, plan, facts, pins, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer path.Close()
	scope, err := plan.SocketScope()
	if err != nil {
		t.Fatal(err)
	}
	if err := path.Renew(ctx, facts, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	observeFixture := func(context.Context) (planner.Facts, error) { return facts, nil }
	if err := path.WithSocket(ctx, observeFixture, scope, strings.Repeat("f", 64), -1, func() error {
		t.Fatal("stale path accessed the mark action")
		return nil
	}); err == nil {
		t.Fatal("stale generation was not rejected")
	}
	dialer := net.Dialer{Control: func(_, _ string, raw syscall.RawConn) error {
		var admission error
		err := raw.Control(func(socket uintptr) {
			admission = path.WithSocket(ctx, observeFixture, scope, path.Generation(), int(socket), func() error {
				return unix.SetsockoptInt(int(socket), unix.SOL_SOCKET, unix.SO_MARK, int(plan.SocketMark()))
			})
		})
		return errors.Join(err, admission)
	}}
	request := []byte("GET /r8b1 HTTP/1.1\r\nHost: example.invalid\r\nConnection: close\r\n\r\n")
	for _, flow := range []struct {
		name    string
		dialer  *net.Dialer
		packets int
	}{{"marked A", &dialer, 2}, {"unmarked control", &net.Dialer{}, 1}} {
		conn, err := flow.dialer.DialContext(ctx, "tcp4", listener.Addr().String())
		if err != nil {
			t.Fatalf("%s: %v", flow.name, err)
		}
		upstream, err := listener.AcceptTCP()
		if err != nil {
			_ = conn.Close()
			t.Fatal(err)
		}
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		_ = upstream.SetReadDeadline(time.Now().Add(time.Second))
		sourcePort := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
		_, sendErr := conn.Write(request)
		got := make([]byte, len(request))
		_, readErr := io.ReadFull(upstream, got)
		_ = conn.Close()
		_ = upstream.Close()
		if sendErr != nil || readErr != nil || !bytes.Equal(got, request) {
			t.Fatalf("%s: receiver did not receive the original byte stream: %v, %v", flow.name, sendErr, readErr)
		}
		sizes := zapretWireTCP(t, wire, sourcePort, port, request)
		t.Logf("%s payload lengths on receiver veth: %v", flow.name, sizes)
		if len(sizes) != flow.packets {
			t.Fatalf("%s: received %d data segments, want %d", flow.name, len(sizes), flow.packets)
		}
	}
	if err := path.Renew(ctx, facts, 30*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if err := path.Renew(ctx, facts, time.Second); err == nil {
		t.Fatal("expired egress generation reopened")
	}
	if path.Lifetime().Err() == nil {
		t.Fatal("retired egress generation did not revoke socket admission")
	}
}

func inZapretReceiverNetNS(t *testing.T, ns string, f func()) {
	t.Helper()
	runtime.LockOSThread()
	restore := true
	defer func() {
		if restore {
			runtime.UnlockOSThread()
		}
	}()
	self, err := os.Open("/proc/thread-self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	defer self.Close()
	other, err := os.Open(ns)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := unix.Setns(int(other.Fd()), unix.CLONE_NEWNET); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Setns(int(self.Fd()), unix.CLONE_NEWNET); err != nil {
			restore = false
			t.Fatalf("restore sender namespace: %v", err)
		}
	}()
	f()
}

func zapretWireTCP(t *testing.T, fd int, source, dest uint16, request []byte) []int {
	t.Helper()
	var sample [2048]byte
	type segment struct {
		seq  uint32
		data []byte
	}
	var segments []segment
	for {
		n, _, err := unix.Recvfrom(fd, sample[:], unix.MSG_DONTWAIT)
		if errors.Is(err, unix.EAGAIN) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if n < 40 || sample[0]>>4 != 4 || sample[9] != unix.IPPROTO_TCP {
			continue
		}
		ihl, total := int(sample[0]&15)*4, int(binary.BigEndian.Uint16(sample[2:4]))
		if ihl < 20 || ihl+20 > total || total > n {
			continue
		}
		tcp := sample[ihl:total]
		if binary.BigEndian.Uint16(tcp[:2]) != source || binary.BigEndian.Uint16(tcp[2:4]) != dest {
			continue
		}
		off := int(tcp[12]>>4) * 4
		if off >= 20 && off < len(tcp) {
			segments = append(segments, segment{binary.BigEndian.Uint32(tcp[4:8]), bytes.Clone(tcp[off:])})
		}
	}
	if len(segments) == 0 {
		t.Fatal("no matching data segments on receiver veth")
	}
	slices.SortFunc(segments, func(a, b segment) int { return int(int32(a.seq - b.seq)) })
	var joined []byte
	var sizes []int
	next := segments[0].seq
	for _, part := range segments {
		if part.seq != next {
			t.Fatalf("unexpected gap or replay at TCP sequence %d", part.seq)
		}
		joined = append(joined, part.data...)
		sizes = append(sizes, len(part.data))
		next += uint32(len(part.data))
	}
	if !bytes.Equal(joined, request) {
		t.Fatal("wire segments do not reconstruct the request")
	}
	return sizes
}
