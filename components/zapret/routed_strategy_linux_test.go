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
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"golang.org/x/sys/unix"
)

// TestZapretRoutedStrategyWire exercises the real engine and guard, not the
// installed Panel or privileged broker lifecycle. Socket admission below is
// fixture-owned; the only destination is another disposable namespace.
func TestZapretRoutedStrategyWire(t *testing.T) {
	testZapretRoutedStrategyWire(t, "expiry")
}

func TestZapretRoutedPublicationFailure(t *testing.T) {
	testZapretRoutedStrategyWire(t, "publication failure")
}

func TestZapretRoutedAuthorityRotation(t *testing.T) {
	testZapretRoutedStrategyWire(t, "authority rotation")
}

func testZapretRoutedStrategyWire(t *testing.T, fault string) {
	t.Helper()
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
	const senderIP, gatewayIP, receiverIP = "192.0.2.1", "192.0.2.2", "198.51.100.2"
	run("", "ip", "link", "add", tx, "type", "veth", "peer", "name", rx)
	run("", "ip", "link", "set", rx, "netns", pid)
	run("", "ip", "addr", "add", senderIP+"/30", "dev", tx)
	run("", "ip", "link", "set", tx, "up")
	run("", "nsenter", "-t", pid, "-n", "--", "ip", "addr", "add", gatewayIP+"/30", "dev", rx)
	run("", "nsenter", "-t", pid, "-n", "--", "ip", "addr", "add", receiverIP+"/32", "dev", rx)
	run("", "nsenter", "-t", pid, "-n", "--", "ip", "link", "set", rx, "up")
	run("", "ip", "-4", "route", "replace", "default", "via", gatewayIP, "dev", tx)
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
	egress, err := planner.ObserveZapretLinuxEgress(ctx, int32(dev.Index), true, false)
	if err != nil {
		t.Fatal(err)
	}
	facts := planner.Facts{
		HostRef: hostref.Local(), Revision: revision, CapabilityRevision: revision, InventoryComplete: true,
		Capability: platform.PlatformFact{Availability: platform.Available, Backend: planner.ZapretInstalledBackend},
		Zapret: &planner.ZapretFacts{OutboundRef: resolved.Intent.OutboundRef, OutboundRevision: revision, Provider: "sing-box",
			DirectSocket: true, SocketMark: true, NFQueue: true, ConntrackAccounting: true, EgressGuard: true,
			InstalledEgress: &egress},
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
	scope, err := plan.SocketScope()
	if err != nil {
		t.Fatal(err)
	}
	prepare := func() *nftables.ZapretNativePath {
		t.Helper()
		path, err := nftables.PrepareZapretNativePath(ctx, plan, facts, pins, identity, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = path.Close() })
		return path
	}
	observeFixture := func(context.Context) (planner.Facts, error) { return facts, nil }
	assertPinCount := func(want int) {
		t.Helper()
		entries, err := os.ReadDir(pins)
		if err != nil || len(entries) != want {
			t.Fatalf("expected %d generation pins, found %v (%v)", want, entries, err)
		}
	}
	failed := prepare()
	assertPinCount(0)
	if err := failed.Renew(ctx, observeFixture, time.Second); err == nil || failed.AdmissionReady(scope, failed.Generation()) {
		t.Fatal("uncommitted generation opened a lease or became ready")
	}
	if err := failed.WithSocket(ctx, observeFixture, scope, failed.Generation(), -1, func() error {
		t.Fatal("uncommitted path accessed the mark action")
		return nil
	}); err == nil {
		t.Fatal("uncommitted generation did not reject admission")
	}
	writeFailure := errors.New("injected commit failure")
	if err := failed.Commit(ctx, observeFixture, func(_ context.Context, cp nftables.ZapretPathCheckpoint) error {
		assertPinCount(0)
		if cp.GuardLinks[0] == 0 || cp.GuardLinks[1] == 0 || cp.GuardLinks[0] == cp.GuardLinks[1] {
			t.Fatalf("staged links have no exact kernel identities: %v", cp.GuardLinks)
		}
		return writeFailure
	}, func(context.Context, nftables.ZapretPathCheckpoint) error {
		t.Fatal("failed commit attempted to authorize a lease")
		return nil
	}); !errors.Is(err, writeFailure) || failed.Lifetime().Err() == nil || failed.AdmissionReady(scope, failed.Generation()) {
		t.Fatalf("commit failure did not retire the exact staged path: %v", err)
	}
	assertPinCount(0)
	failedLease := prepare()
	if err := failedLease.Commit(ctx, observeFixture, func(context.Context, nftables.ZapretPathCheckpoint) error { return nil },
		func(context.Context, nftables.ZapretPathCheckpoint) error {
			assertPinCount(2)
			return writeFailure
		}); err != nil {
		t.Fatal(err)
	}
	if err := failedLease.Renew(ctx, observeFixture, time.Second); !errors.Is(err, writeFailure) ||
		failedLease.AdmissionReady(scope, failedLease.Generation()) || failedLease.Lifetime().Err() == nil {
		t.Fatalf("first-lease write failure did not prevent admission: %v", err)
	}
	assertPinCount(0)
	idle := prepare() // A failed commit must release its own unadmitted table and pins.
	journalDir := filepath.Join(t.TempDir(), "solovey-ui-broker")
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Fatal(err)
	}
	bootID := strings.TrimSpace(string(boot))
	j, err := privilegedbroker.OpenFileJournal(journalDir, bootID)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.CommitZapretPath(ctx, idle, observeFixture, revision); err != nil {
		t.Fatal(err)
	}
	assertPinCount(2)
	if err := j.RetireZapretUnleased(ctx, idle, strings.Repeat("b", 64)); err == nil || idle.Lifetime().Err() != nil {
		t.Fatal("unrelated peer authority retired the idle generation")
	}
	if err := j.RetireZapretUnleased(ctx, idle, revision); err != nil || idle.Lifetime().Err() == nil || idle.AdmissionReady(scope, idle.Generation()) {
		t.Fatalf("unadmitted generation did not retire cleanly: %v", err)
	}
	assertPinCount(0)
	// Leave the prepared receipt behind, but close the real kernel owner
	// without recording a retirement. Reopen in the same boot and require a
	// fresh table and fresh links rather than adoption of the old resources.
	orphan := prepare()
	if err := j.CommitZapretPath(ctx, orphan, observeFixture, revision); err != nil {
		t.Fatal(err)
	}
	assertPinCount(2)
	if err := orphan.Close(); err != nil {
		t.Fatal(err)
	}
	assertPinCount(0)
	j, err = privilegedbroker.OpenFileJournal(journalDir, bootID)
	if err != nil {
		t.Fatal(err)
	}
	path := prepare()
	if err := j.CommitZapretPath(ctx, path, observeFixture, revision); err != nil {
		t.Fatalf("unleased receipt without live resources blocked same-boot replacement: %v", err)
	}
	t.Log("ZAPRET_UNLEASED_SAME_BOOT_REOPEN=PASS")
	if err := j.RetireZapretUnleased(ctx, orphan, revision); err == nil || path.Lifetime().Err() != nil {
		t.Fatal("stale retirement cleared the replacement generation")
	}
	if path.AdmissionReady(scope, path.Generation()) {
		t.Fatal("durable record became an admission grant before the first lease")
	}
	if err := path.Renew(ctx, observeFixture, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if !path.AdmissionReady(scope, path.Generation()) {
		t.Fatal("committed and leased generation is not ready")
	}
	if err := j.RetireZapretUnleased(ctx, path, revision); err == nil || path.Lifetime().Err() != nil {
		t.Fatal("first kernel lease permitted retirement before Panel socket admission")
	}
	t.Log("ZAPRET_FIRST_LEASE_BEFORE_ADMISSION_FENCE=PASS")
	t.Log("ZAPRET_COMMIT_GATE=PASS (uncommitted admission denied, failed commit retired, journal then lease)")
	t.Log("ZAPRET_PIN_AFTER_DURABLE_COMMIT=PASS")
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
	type stream struct {
		sender, receiver net.Conn
	}
	var a, b stream
	for _, flow := range []struct {
		name    string
		dialer  *net.Dialer
		packets int
		stream  *stream
	}{{"marked A", &dialer, 2, &a}, {"unmarked control", &net.Dialer{}, 1, &b}} {
		conn, err := flow.dialer.DialContext(ctx, "tcp4", listener.Addr().String())
		if err != nil {
			t.Fatalf("%s: %v", flow.name, err)
		}
		defer conn.Close()
		upstream, err := listener.AcceptTCP()
		if err != nil {
			t.Fatal(err)
		}
		defer upstream.Close()
		*flow.stream = stream{conn, upstream}
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		_ = upstream.SetReadDeadline(time.Now().Add(time.Second))
		sourcePort := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
		_, sendErr := conn.Write(request)
		got := make([]byte, len(request))
		_, readErr := io.ReadFull(upstream, got)
		if sendErr != nil || readErr != nil || !bytes.Equal(got, request) {
			t.Fatalf("%s: receiver did not receive the original byte stream: %v, %v", flow.name, sendErr, readErr)
		}
		sizes := zapretWireTCP(t, wire, sourcePort, port, request)
		t.Logf("%s payload lengths on receiver veth: %v", flow.name, sizes)
		if len(sizes) != flow.packets {
			t.Fatalf("%s: received %d data segments, want %d", flow.name, len(sizes), flow.packets)
		}
	}
	if err := j.RetireZapretUnleased(ctx, path, revision); err == nil || path.Lifetime().Err() != nil {
		t.Fatal("retirement released a checkpoint after socket enrollment")
	}
	t.Log("ZAPRET_IDLE_RETIRE_REOPEN=PASS; STALE_AND_ADMITTED_RELEASE=DENIED")
	if err := path.Renew(ctx, observeFixture, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	server := &privilegedbroker.Server{Journal: j, BootID: bootID}
	publishWithWrongPeer := func() error {
		// Reject at the journal gate before touching the installed socket root.
		wrong := privilegedbroker.PeerIdentity{BootID: bootID, Revision: strings.Repeat("b", 64)}
		return server.ServeZapretSockets(ctx, scope, wrong, path, observeFixture, func(context.Context, string) error {
			t.Fatal("listener with the wrong journal peer was published")
			return nil
		})
	}
	switch fault {
	case "publication failure":
		if err := publishWithWrongPeer(); err == nil || !errors.Is(path.Lifetime().Err(), context.Canceled) || path.AdmissionReady(scope, path.Generation()) {
			t.Fatalf("publication failure left a live generation: %v", err)
		}
	case "authority rotation":
		rotateZapretAuthority(t, ctx, j, bootID, revision, scope, path, observeFixture)
	case "expiry":
		if !path.ClaimSocketPublication(scope) {
			t.Fatal("first publisher could not claim the ready generation")
		}
		if err := publishWithWrongPeer(); err == nil || path.Lifetime().Err() != nil || !path.AdmissionReady(scope, path.Generation()) {
			t.Fatalf("a second publisher retired the claimed generation: %v", err)
		}
		// Both sockets stay open and the owner does not revoke the kernel lease
		// or remove the live table. A renewal cannot outlast the grant.
		select {
		case <-path.Lifetime().Done():
		case <-ctx.Done():
			t.Fatal("generation did not expire in the routed fixture")
		}
		if !errors.Is(path.Lifetime().Err(), context.DeadlineExceeded) || path.AdmissionReady(scope, path.Generation()) {
			t.Fatal("generation did not close admission at its grant deadline")
		}
	default:
		t.Fatalf("unknown fault %q", fault)
	}
	if fault != "expiry" {
		assertPinCount(2) // Old sockets still require the independent deny hooks.
		cmd := exec.CommandContext(ctx, "nft", "list", "tables")
		if listing, err := cmd.CombinedOutput(); err != nil || bytes.Contains(listing, []byte(planner.ZapretInstalledTable)) {
			t.Fatalf("%s did not release the owned table: %v\n%s", fault, err, listing)
		}
		if _, err := nftables.ObserveZapretQueueOwner(plan, identity); err != nil {
			t.Fatalf("%s did not keep the engine and queue live: %v", fault, err)
		}
	}
	for _, flow := range []struct {
		stream
		allowed bool
	}{{a, false}, {b, true}} {
		_ = flow.sender.SetWriteDeadline(time.Now().Add(time.Second))
		_, sendErr := flow.sender.Write([]byte{0x61})
		_ = flow.receiver.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		var marker [1]byte
		n, readErr := flow.receiver.Read(marker[:])
		if flow.allowed && (sendErr != nil || readErr != nil || n != 1 || marker[0] != 0x61) {
			t.Fatalf("closed admission affected the unmarked socket: sent %v, read %d (%v)", sendErr, n, readErr)
		}
		if !flow.allowed && (n != 0 || !errors.Is(readErr, os.ErrDeadlineExceeded)) {
			t.Fatalf("old marked socket was not blocked after admission closed: read %d (%v)", n, readErr)
		}
	}
	switch fault {
	case "publication failure":
		t.Log("ZAPRET_PUBLICATION_ROLLBACK=PASS (owned table removed, deny hooks kept, existing A blocked and B unaffected)")
	case "authority rotation":
		t.Log("ZAPRET_AUTHORITY_ROTATION=PASS (old grant revoked before publication drained, existing A blocked and B unaffected)")
	case "expiry":
		t.Log("ZAPRET_SINGLE_PUBLISHER=PASS; ZAPRET_GRANT_DEADLINE=PASS (already-issued A denied; existing B unaffected)")
	}
	if err := path.Renew(ctx, observeFixture, time.Second); err == nil {
		t.Fatal("expired egress generation reopened")
	}
	if path.Lifetime().Err() == nil {
		t.Fatal("retired egress generation did not revoke socket admission")
	}
}

func rotateZapretAuthority(t *testing.T, ctx context.Context, journal *privilegedbroker.FileJournal, boot, peerRevision string,
	scope planner.ZapretSocketScope, path *nftables.ZapretNativePath, observe func(context.Context) (planner.Facts, error)) {
	t.Helper()
	self, err := processevidence.Observe(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	attestor := func(unit string) privilegedbroker.ManifestAttestor {
		t.Helper()
		manifest, err := privilegedbroker.FinalizeManifest(privilegedbroker.Manifest{Schema: privilegedbroker.ManifestSchemaSystemd,
			Clients: []privilegedbroker.ClientManifest{{Name: "panel", UID: 65534, GID: 65534, Roles: []privilegedbroker.Role{privilegedbroker.RolePanel},
				Executable: self.Executable, ExecutableDigest: self.ExeDigest, Device: self.ExeDevice, Inode: self.ExeInode,
				CgroupUnit: unit, CgroupPolicy: privilegedbroker.CgroupRequired, CgroupAuthorityRevision: privilegedbroker.CgroupAuthorityRevisionV1}},
		})
		if err != nil {
			t.Fatal(err)
		}
		a, err := privilegedbroker.NewManifestAttestor(manifest)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	before, after := attestor("sui-fixture-before.service"), attestor("sui-fixture-after.service")
	server, err := privilegedbroker.NewServer(privilegedbroker.NewRegistry(), journal, before, boot)
	if err != nil {
		t.Fatal(err)
	}
	// This fixture has already enrolled its A socket. It exercises revocation
	// of that generation, not attestation of a new capless client.
	peer := privilegedbroker.PeerIdentity{BootID: boot, Revision: peerRevision, ManifestRevision: before.AuthorityRevision()}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	published, done := make(chan string, 1), make(chan error, 1)
	go func() {
		done <- server.ServeZapretSockets(ctx, scope, peer, path, observe, func(publication context.Context, name string) error {
			published <- name
			select {
			case <-path.Lifetime().Done():
				return publication.Err()
			case <-ctx.Done():
				return errors.New("generation did not retire while publication was pending")
			}
		})
	}()
	var name string
	select {
	case name = <-published:
	case err := <-done:
		t.Fatalf("listener did not reach publication: %v", err)
	case <-ctx.Done():
		t.Fatal("listener did not reach publication before the test deadline")
	}
	if err := server.RotateAuthority(ctx, after, after.AuthorityRevision()); err != nil {
		t.Fatalf("authority rotation did not drain the old generation: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected listener exit: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("listener did not exit after rotation")
	}
	if server.AuthorityRevision() != after.AuthorityRevision() || !errors.Is(path.Lifetime().Err(), context.Canceled) || path.AdmissionReady(scope, path.Generation()) {
		t.Fatal("old generation remained live after authority rotation")
	}
	if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old generation endpoint still exists: %v", err)
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
