//go:build linux && zapretproof && !minimal

package zapret

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-component-zapret/binding"
	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-host/outbounds"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	platform "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	M "github.com/sagernet/sing/common/metadata"
)

type zapretFixtureSource struct{ value strategy.Strategy }

func (s zapretFixtureSource) LookupStrategy(ref string) (strategy.Strategy, bool) {
	return s.value, s.value.ID == ref
}

func TestZapretOutboundNamespace(t *testing.T) {
	engineRoot := os.Getenv("SOLOVEY_TEST_ENGINE_ROOT")
	parent := os.Getenv("SOLOVEY_TEST_PARENT_NETNS")
	if engineRoot == "" || parent == "" {
		t.Fatal("qualification requires pinned engine and a disposable network namespace")
	}
	current, readErr := os.Readlink("/proc/self/ns/net")
	initNS, initErr := os.Readlink("/proc/1/ns/net")
	interfaces, ifaceErr := net.Interfaces()
	if readErr != nil || initErr != nil || current == parent || current == initNS || os.Geteuid() != 0 ||
		ifaceErr != nil || len(interfaces) != 1 || interfaces[0].Name != "lo" || interfaces[0].Flags&net.FlagLoopback == 0 {
		t.Fatal("refusing to modify anything outside an isolated loopback-only namespace")
	}
	if _, err := os.Stat("/proc/net/netfilter/nfnetlink_queue"); err != nil {
		t.Fatalf("PACKET_PATH_NOT_YET_PROVEN: kernel NFQUEUE inventory unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	run := func(input string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "nft", args...)
		cmd.Stdin = strings.NewReader(input)
		return cmd.CombinedOutput()
	}
	nft := func(input string, args ...string) []byte {
		t.Helper()
		output, err := run(input, args...)
		if err != nil {
			t.Fatalf("nft %v: %v\n%s", args, err, output)
		}
		return output
	}
	if value, err := os.ReadFile("/proc/sys/net/netfilter/nf_conntrack_acct"); err != nil || strings.TrimSpace(string(value)) != "1" {
		t.Fatal("PACKET_PATH_NOT_YET_PROVEN: namespace conntrack accounting unavailable")
	}
	if tables := nft("", "list", "tables"); len(bytes.TrimSpace(tables)) != 0 {
		t.Fatal("namespace already owns firewall resources")
	}
	nft("table inet fixture_other {\n counter untouched {}\n}\n", "-f", "-")
	other := nft("", "list", "table", "inet", "fixture_other")

	type endpoint struct {
		address string
		proto   intent.PacketProtocol
		port    uint16
	}
	var endpoints []endpoint
	var profiles []strategy.Profile
	for _, family := range []string{"4", "6"} {
		host := "127.0.0.1"
		if family == "6" {
			host = "::1"
		}
		for _, proto := range []intent.PacketProtocol{intent.PacketProtocolTCP, intent.PacketProtocolUDP} {
			addr, port := zapretEcho(t, string(proto)+family, net.JoinHostPort(host, "0"))
			endpoints = append(endpoints, endpoint{addr, proto, port})
			profiles = append(profiles, strategy.Profile{ID: string(proto) + family, Protocol: proto, Ports: []uint16{port}})
		}
	}
	value, err := strategy.Seal(strategy.Strategy{Schema: strategy.Schema, ID: "passive-probe", Profiles: profiles})
	if err != nil {
		t.Fatal(err)
	}
	digest := func(input string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(input))) }
	resolved, err := binding.Resolve(outbounds.ZapretBinding{OutboundRef: "direct-a", Enabled: true, StrategyRef: value.ID},
		binding.DirectOutbound{Host: hostref.Local(), Ref: "direct-a", Type: "direct", Revision: digest("direct-a unmarked")}, zapretFixtureSource{value}, true)
	if err != nil {
		t.Fatal(err)
	}
	queueList, err := os.ReadFile("/proc/net/netfilter/nfnetlink_queue")
	if err != nil || len(bytes.TrimSpace(queueList)) != 0 {
		t.Fatal("expected an empty isolated NFQUEUE inventory")
	}
	facts := planner.Facts{HostRef: hostref.Local(), Revision: digest(current + string(other)), CapabilityRevision: digest("isolated-v1"),
		InventoryComplete: true, Capability: platform.PlatformFact{Availability: platform.Available, Backend: planner.ZapretNamespaceBackend},
		Allocations: []planner.Allocation{{Ownership: planner.Ownership{Allocation: "inet:fixture_other"}}},
		Zapret: &planner.ZapretFacts{OutboundRef: resolved.Intent.OutboundRef, OutboundRevision: resolved.Intent.InputRevision,
			Provider: "sing-box", DirectSocket: true, SocketMark: true, NFQueue: true, ConntrackAccounting: true}}
	plan, err := planner.CompileZapret(resolved.Intent, facts)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := nftables.RenderZapretProbe(plan)
	if err != nil {
		t.Fatal(err)
	}
	// A transaction rejected by the kernel must not leave any part applied.
	if output, invalidErr := run(candidate+"add rule inet missing_table missing_chain accept\n", "-f", "-"); invalidErr == nil {
		t.Fatalf("invalid atomic transaction committed: %s", output)
	}
	if tables := string(nft("", "list", "tables")); strings.Contains(tables, planner.ZapretProbeTable) || !bytes.Equal(other, nft("", "list", "table", "inet", "fixture_other")) {
		t.Fatal("failed apply changed the namespace baseline")
	}
	nft(candidate, "--check", "-f", "-")
	accepted, err := planner.Accept(plan.Plan(), facts, digest(candidate))
	if err != nil || planner.CheckAccepted(accepted, facts, digest(candidate)) != nil {
		t.Fatalf("candidate not authorized by exact Planner receipt: %v", err)
	}
	argv, err := value.Compile(engineRoot, plan.Queue(), plan.SocketMark(), plan.LoopMark())
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "nfqws2.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd := exec.CommandContext(ctx, filepath.Join(engineRoot, "nfqws2"), argv...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = engineRoot, log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		if t.Failed() {
			output, _ := os.ReadFile(logPath)
			t.Logf("nfqws2: %s", output)
		}
	}()
	ready := func() bool {
		data, err := os.ReadFile("/proc/net/netfilter/nfnetlink_queue")
		return err == nil && strings.HasPrefix(strings.TrimSpace(string(data)), strconv.Itoa(int(plan.Queue()))+" ")
	}
	for deadline := time.Now().Add(4 * time.Second); !ready(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("PACKET_PATH_NOT_YET_PROVEN: no listener in the allocated NFQUEUE")
		}
	}
	nft(candidate, "-f", "-")
	defer func() { _, _ = run("", "delete", "table", "inet", planner.ZapretProbeTable) }()

	core := coreruntime.NewCore()
	configuration := fmt.Sprintf(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct-a","routing_mark":"0x%x"},{"type":"direct","tag":"direct-b"}],"route":{"final":"direct-b"}}`, plan.SocketMark())
	if err := core.Start([]byte(configuration)); err != nil {
		t.Fatal(err)
	}
	defer core.Stop()
	dial := func(ctx context.Context, name, network, address string) (net.Conn, error) {
		if name == "unmarked" {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
		return core.DialOutbound(ctx, name, network, M.ParseSocksaddr(address))
	}
	for _, target := range endpoints {
		for _, name := range []string{"direct-a", "direct-b", "unmarked"} {
			before := zapretCounters(t, nft("", "-j", "list", "table", "inet", planner.ZapretProbeTable))
			conn, err := dial(ctx, name, string(target.proto), target.address)
			zapretRoundTrip(t, conn, err)
			after := zapretCounters(t, nft("", "-j", "list", "table", "inet", planner.ZapretProbeTable))
			seenKey, captureKey := string(target.proto)+"_seen", string(target.proto)+"_captured"
			seen, captured := after[seenKey]-before[seenKey], after[captureKey]-before[captureKey]
			t.Logf("%s %s %s: captured=%d observed=%d queue=%d", target.proto, target.address, name, captured, seen, plan.Queue())
			if seen < 1 || captured > seen || (captured > 0) != (name == "direct-a") {
				t.Fatal("packet capture escaped its admitted Direct scope or the expected flow was lost")
			}
		}
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	stopped = true
	if err != nil || ready() {
		t.Fatalf("engine did not release its queue: %v", err)
	}
	// Loss of the queue consumer must not turn the admitted outbound into
	// plain Direct, nor interrupt the unmarked outbound to the same peer.
	blockedCtx, cancelBlocked := context.WithTimeout(ctx, 750*time.Millisecond)
	target := endpoints[0] // TCP/IPv4
	conn, err := dial(blockedCtx, "direct-a", "tcp", target.address)
	cancelBlocked()
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Fatal("engine loss silently downgraded A to plain Direct")
	}
	conn, err = dial(ctx, "direct-b", "tcp", target.address)
	zapretRoundTrip(t, conn, err)
	t.Log("QUEUE_LOSS_FAIL_CLOSED: Direct A did not connect; Direct B echoed")
	if err := core.Stop(); err != nil {
		t.Fatal(err)
	}
	nft("", "delete", "table", "inet", planner.ZapretProbeTable)
	if strings.Contains(string(nft("", "list", "tables")), planner.ZapretProbeTable) ||
		!bytes.Equal(other, nft("", "list", "table", "inet", "fixture_other")) {
		t.Fatalf("stop did not release the exact probe resources: %v", err)
	}
	t.Log("PACKET_PATH_PROVEN: TCP/UDP IPv4/IPv6, failed apply atomic, stop scoped, no external traffic")
}

func zapretEcho(t *testing.T, network, addr string) (string, uint16) {
	t.Helper()
	if strings.HasPrefix(network, "tcp") {
		listener, err := net.Listen(network, addr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				_ = conn.SetDeadline(time.Now().Add(time.Second))
				_, _ = io.CopyN(conn, conn, 4)
				_ = conn.Close()
			}
		}()
		return listener.Addr().String(), uint16(listener.Addr().(*net.TCPAddr).Port)
	}
	listener, err := net.ListenPacket(network, addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		var data [64]byte
		for {
			n, addr, err := listener.ReadFrom(data[:])
			if err != nil {
				return
			}
			_, _ = listener.WriteTo(data[:n], addr)
		}
	}()
	return listener.LocalAddr().String(), uint16(listener.LocalAddr().(*net.UDPAddr).Port)
}

func zapretRoundTrip(t *testing.T, conn net.Conn, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("echo")); err != nil {
		t.Fatal(err)
	}
	var data [4]byte
	if _, err := io.ReadFull(conn, data[:]); err != nil || string(data[:]) != "echo" {
		t.Fatalf("echo data did not pass the queue: %q %v", data, err)
	}
}

func zapretCounters(t *testing.T, raw []byte) map[string]uint64 {
	t.Helper()
	var listing struct {
		NFTables []struct {
			Counter *struct {
				Name    string
				Packets uint64
			}
		}
	}
	if err := json.Unmarshal(raw, &listing); err != nil {
		t.Fatal(err)
	}
	result := map[string]uint64{}
	for _, item := range listing.NFTables {
		if item.Counter != nil {
			result[item.Counter.Name] = item.Counter.Packets
		}
	}
	if len(result) != 4 {
		t.Fatalf("wrong probe counters: %v", result)
	}
	return result
}
