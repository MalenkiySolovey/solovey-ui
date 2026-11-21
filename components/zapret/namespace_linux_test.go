//go:build linux && zapretproof && !minimal

package zapret

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-component-zapret/binding"
	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/network/intent"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	platform "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"github.com/MalenkiySolovey/solovey-ui/network/nftables"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	M "github.com/sagernet/sing/common/metadata"
	"golang.org/x/sys/unix"
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
	rowA := model.Outbound{Type: "direct", Tag: "direct-a", Options: json.RawMessage(`{"connect_timeout":"3s","zapret":{"outboundRef":"direct-a","enabled":true,"strategyRef":"passive-probe"}}`)}
	rowB := model.Outbound{Type: "direct", Tag: "direct-b", Options: json.RawMessage(`{"zapret":{"outboundRef":"direct-b","enabled":false}}`)}
	attachment, revision, err := entityoutbounds.ZapretProbeInput(rowA)
	if err != nil {
		t.Fatal(err)
	}
	digest := func(input string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(input))) }
	resolved, err := binding.Resolve(attachment, binding.DirectOutbound{Host: hostref.Local(), Ref: rowA.Tag,
		Type: rowA.Type, Revision: revision}, zapretFixtureSource{value}, true)
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
			Provider: "sing-box", DirectSocket: true, SocketMark: true, NFQueue: true, ConntrackAccounting: true, EgressGuard: true}}
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
	probe, err := nftables.NewZapretProbe(plan, parent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = probe.Close() })
	accepted, err := probe.Validate(ctx, facts)
	if err != nil {
		t.Fatalf("exact Planner candidate validation failed: %v", err)
	}
	journalRoot := filepath.Join(t.TempDir(), "solovey-ui-broker")
	change := newZapretProbeChange(t, ctx, probe, accepted, facts, journalRoot, plan.Plan().Revision())
	if err := change.call(ctx, zapretApply, 1); !probeBrokerFailure(err, broker.CodeValidation) {
		t.Fatalf("Apply admitted without a queue consumer: %v", err)
	}
	if tables := string(nft("", "list", "tables")); strings.Contains(tables, planner.ZapretProbeTable) {
		t.Fatal("queue-less Apply altered the namespace")
	}
	argv, err := value.Compile(engineRoot, plan.Queue(), plan.QueueMark(), plan.LoopMark())
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
		_, err := probe.QueuePeer()
		return err == nil
	}
	for deadline := time.Now().Add(4 * time.Second); !ready(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("PACKET_PATH_NOT_YET_PROVEN: no listener in the allocated NFQUEUE")
		}
	}
	// Both axes must hold: a live queue alone cannot authorize the plan if
	// the ruleset changed after Validate. Protect even unrelated resources.
	nft("", "add", "counter", "inet", "fixture_other", "intervening")
	if err := change.call(ctx, zapretApply, 2); !probeBrokerFailure(err, broker.CodeValidation) {
		t.Fatalf("Apply admitted a stale before-image: %v", err)
	}
	if tables := string(nft("", "list", "tables")); strings.Contains(tables, planner.ZapretProbeTable) {
		t.Fatal("stale Apply created the probe table")
	}
	nft("", "delete", "counter", "inet", "fixture_other", "intervening")
	change.require(t, ctx, zapretApply, 3)

	if _, err := entityoutbounds.ProjectZapretProbe(rowA, planner.ZapretPlan{}); !errors.Is(err, entityoutbounds.ErrZapretPacketPathUnqualified) {
		t.Fatalf("enabled persisted Direct projected without the Planner: %v", err)
	}
	if _, err := entityoutbounds.ProjectZapretProbe(rowB, plan); !errors.Is(err, entityoutbounds.ErrZapretPacketPathUnqualified) {
		t.Fatalf("an OFF binding borrowed the active mark: %v", err)
	}
	changedA := rowA
	changedA.Options = bytes.ReplaceAll(rowA.Options, []byte(`"3s"`), []byte(`"2s"`))
	if _, err := entityoutbounds.ProjectZapretProbe(changedA, plan); !errors.Is(err, entityoutbounds.ErrZapretPacketPathUnqualified) {
		t.Fatalf("changed Direct reused the wrong Planner allocation: %v", err)
	}
	directA, err := entityoutbounds.ProjectZapretProbe(rowA, plan)
	if err != nil {
		t.Fatal(err)
	}
	directB, err := entityoutbounds.ProjectZapretProbe(rowB, planner.ZapretPlan{})
	if err != nil {
		t.Fatal(err)
	}
	core := coreruntime.NewCore()
	configuration := fmt.Sprintf(`{"log":{"disabled":true},"outbounds":[%s,%s],"route":{"final":"direct-b"}}`, directA, directB)
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
			var want uint64
			if name == "direct-a" {
				want = seen
			}
			if seen < 1 || captured != want {
				t.Fatal("packet capture escaped its admitted Direct scope or the expected flow was lost")
			}
		}
	}
	committed := change.require(t, ctx, zapretCommit, 4)
	// Reopen the actual on-disk owner journal, rather than reusing an
	// in-memory Apply result as either checkpoint or compensation authority.
	change.stop()
	change = newZapretProbeChange(t, ctx, probe, accepted, facts, journalRoot, plan.Plan().Revision())
	checkpoint, receipt, err := change.load(zapretCommit)
	if err != nil || receipt.ReceiptDigest != committed.ReceiptDigest || probe.Verify(ctx, checkpoint) != nil {
		t.Fatalf("verified checkpoint did not survive journal reopen: %v", err)
	}
	t.Log("CHANGE_COMMITTED: Planner receipt, attested Unix broker Apply, packet verification and reopened checkpoint agree")
	beforeCapless := zapretCounters(t, nft("", "-j", "list", "table", "inet", planner.ZapretProbeTable))
	qualifyZapretProtectPath(t, ctx, directA, directB, plan.SocketMark(), endpoints[0].address, endpoints[1].address)
	afterCapless := zapretCounters(t, nft("", "-j", "list", "table", "inet", planner.ZapretProbeTable))
	if afterCapless["tcp_captured"] <= beforeCapless["tcp_captured"] || afterCapless["udp_captured"]-beforeCapless["udp_captured"] != 1 {
		t.Fatal("the capless Direct TCP/UDP traffic was not observed at the bounded queue")
	}
	// Model replacement of a different table, not a global ruleset flush.
	nft("delete table inet fixture_other\ncreate table inet fixture_other\nadd counter inet fixture_other reloaded\n", "-f", "-")
	other = nft("", "list", "table", "inet", "fixture_other")
	if err := probe.VerifyCurrent(ctx, checkpoint); err != nil {
		t.Fatalf("unrelated table replacement invalidated the exact owned generation: %v", err)
	}
	reloadTarget := endpoints[1] // UDP/IPv4; a new, short flow.
	beforeReload := zapretCounters(t, nft("", "-j", "list", "table", "inet", planner.ZapretProbeTable))
	conn, err := dial(ctx, "direct-a", "udp", reloadTarget.address)
	zapretRoundTrip(t, conn, err)
	afterReload := zapretCounters(t, nft("", "-j", "list", "table", "inet", planner.ZapretProbeTable))
	if afterReload["udp_captured"]-beforeReload["udp_captured"] != 1 {
		t.Fatal("unrelated table replacement disturbed the admitted path")
	}
	t.Log("FOREIGN_TABLE_RELOAD_SCOPED: the exact Zapret table still queued Direct A after independent table replacement")
	// Leave an already-used UDP socket alive across the loss of its table.
	var established net.Conn
	target := endpoints[0] // TCP/IPv4
	established, err = dial(ctx, "direct-a", "udp", reloadTarget.address)
	if err != nil {
		t.Fatal(err)
	}
	defer established.Close()
	zapretExchange(t, established)
	assertTCPBlocked := func(address string) {
		t.Helper()
		blocked, cancelBlocked := context.WithTimeout(ctx, 450*time.Millisecond)
		defer cancelBlocked()
		conn, err := dial(blocked, "direct-a", "tcp", address)
		if conn != nil {
			_ = conn.Close()
		}
		if err == nil {
			t.Fatal("Direct A silently downgraded to plain Direct")
		}
	}
	assertUDPBlocked := func(conn net.Conn, attempts int) {
		t.Helper()
		if err := conn.SetDeadline(time.Now().Add(400 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		for range attempts {
			_, _ = conn.Write([]byte("echo"))
		}
		var data [4]byte
		if n, _ := conn.Read(data[:]); n != 0 {
			t.Fatal("Direct A leaked a datagram without the admitted path")
		}
	}
	// Remove only the TCP dispatch rule while its queue peer is alive.
	list := nft("", "-j", "-a", "list", "chain", "inet", planner.ZapretProbeTable, "outbound")
	handle := zapretHandoffHandle(t, list, "tcp_captured")
	nft("", "delete", "rule", "inet", planner.ZapretProbeTable, "outbound", "handle", fmt.Sprint(handle))
	assertTCPBlocked(target.address)
	conn, err = dial(ctx, "direct-b", "tcp", target.address)
	zapretRoundTrip(t, conn, err)
	if err := probe.VerifyCurrent(ctx, checkpoint); err == nil {
		t.Fatal("checkpoint admitted a missing dispatch rule")
	}
	changed := nft("", "list", "table", "inet", planner.ZapretProbeTable)
	if err := change.call(ctx, zapretRollback, 5); !probeBrokerFailure(err, broker.CodeRecoveryRequired) ||
		!bytes.Equal(changed, nft("", "list", "table", "inet", planner.ZapretProbeTable)) {
		t.Fatalf("rollback removed or changed an unproven table generation: %v", err)
	}
	t.Log("RULE_LOSS_FAIL_CLOSED: removed the TCP handoff with the queue and Core alive; A blocked, B echoed")
	// No packet count may become an implicit pass after queue loss.
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	stopped = true
	if err != nil || ready() {
		t.Fatalf("engine did not release its queue: %v", err)
	}
	assertUDPBlocked(established, 24)
	conn, err = dial(ctx, "direct-b", "udp", reloadTarget.address)
	zapretRoundTrip(t, conn, err)
	t.Log("QUEUE_LOSS_FAIL_CLOSED: the remaining UDP rule had no queue peer; A could not echo after 24 sends, B echoed")
	nft("", "delete", "table", "inet", planner.ZapretProbeTable)
	for _, target := range endpoints {
		if target.proto == intent.PacketProtocolTCP {
			assertTCPBlocked(target.address)
		} else {
			conn, err := dial(ctx, "direct-a", "udp", target.address)
			if err == nil {
				assertUDPBlocked(conn, 1)
				_ = conn.Close()
			}
		}
		for _, name := range []string{"direct-b", "unmarked"} {
			conn, err := dial(ctx, name, string(target.proto), target.address)
			zapretRoundTrip(t, conn, err)
		}
	}
	assertUDPBlocked(established, 1)
	t.Log("TABLE_LOSS_FAIL_CLOSED: tc blocked fresh TCP/UDP on IPv4/IPv6 and the existing UDP socket; B and foreign traffic echoed")
	change.stop()
	change = newZapretProbeChange(t, ctx, probe, accepted, facts, journalRoot, plan.Plan().Revision())
	if err := change.call(ctx, zapretCommit, 6); !probeBrokerFailure(err, broker.CodeRecoveryRequired) {
		t.Fatalf("restarted broker committed a lost nft generation: %v", err)
	}
	t.Log("CHECKPOINT_REFUSED_AFTER_REOPEN: a durable receipt did not authorize the missing table")
	if err := core.Stop(); err != nil {
		t.Fatal(err)
	}
	_ = established.Close()
	change.require(t, ctx, zapretRollback, 7)
	if _, _, err := change.load(zapretApply); err == nil {
		t.Fatal("verified rollback did not release the Apply authority")
	}
	if strings.Contains(string(nft("", "list", "tables")), planner.ZapretProbeTable) ||
		!bytes.Equal(other, nft("", "list", "table", "inet", "fixture_other")) {
		t.Fatalf("stop did not release the exact probe resources: %v", err)
	}
	if err := probe.VerifyCurrent(ctx, checkpoint); err == nil {
		t.Fatal("the retired checkpoint still authorizes the missing table")
	}
	t.Log("PACKET_PATH_PROVEN: TCP/UDP IPv4/IPv6, failed apply atomic, stop scoped, no external traffic")
}

const (
	zapretApply    broker.Verb = "network.zapret-probe.apply"
	zapretCommit   broker.Verb = "network.zapret-probe.commit"
	zapretRollback broker.Verb = "network.zapret-probe.rollback"
	zapretChangeID             = "zapret-namespace-change"
	zapretFence                = "zapret-namespace-outbound"
)

type zapretProbeChange struct {
	client   *broker.Client
	stop     func()
	journal  *broker.FileJournal
	revision string
}

type zapretProbePayload struct {
	PlanRevision string `json:"planRevision"`
}

func newZapretProbeChange(t *testing.T, ctx context.Context, probe *nftables.ZapretProbe, accepted nftables.ZapretProbeValidation, facts planner.Facts, root, revision string) zapretProbeChange {
	t.Helper()
	unit := os.Getenv("SOLOVEY_TEST_BROKER_UNIT")
	bootID, bootErr := os.ReadFile("/proc/sys/kernel/random/boot_id")
	self, err := processevidence.Observe(os.Getpid())
	if err != nil || bootErr != nil || unit == "" {
		t.Fatalf("qualification broker requires its systemd and executable identity: %v %v", err, bootErr)
	}
	manifest, err := broker.FinalizeManifest(broker.Manifest{Schema: broker.ManifestSchemaSystemd, Clients: []broker.ClientManifest{{
		Name: "zapret-qualification", UID: uint32(self.UID), GID: uint32(self.GID), Executable: self.Executable,
		ExecutableDigest: self.ExeDigest, Device: self.ExeDevice, Inode: self.ExeInode,
		CgroupUnit: unit, CgroupPolicy: broker.CgroupRequired, CgroupAuthorityRevision: broker.CgroupAuthorityRevisionV1,
		Roles: []broker.Role{broker.RolePanel},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	attestor, err := broker.NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	boot := strings.TrimSpace(string(bootID))
	journal, err := broker.OpenFileJournal(root, boot)
	if err != nil {
		t.Fatal(err)
	}
	change := zapretProbeChange{journal: journal, revision: revision}
	registry := broker.NewRegistry()
	for _, verb := range []broker.Verb{zapretApply, zapretCommit, zapretRollback} {
		def := broker.Definition{Role: broker.RolePanel, Mutation: true, RetainResultUntilRelease: verb != zapretRollback}
		if verb == zapretRollback {
			def.ReleasesRetainedResultVerb = zapretApply
		}
		def.Handler = func(ctx context.Context, request broker.Request, _ broker.PeerIdentity) (any, error) {
			var payload zapretProbePayload
			if request.OperationID != zapretChangeID || request.Fence.Resource != zapretFence || request.Fence.Token != revision ||
				broker.DecodeRawPayload(request.Payload, &payload) != nil || payload.PlanRevision != revision {
				return nil, broker.Failure(broker.CodeFence, "probe plan or change fence does not match")
			}
			if verb == zapretApply {
				seal, err := probe.Apply(ctx, accepted, facts)
				if err != nil {
					t.Logf("Zapret probe Apply: %v", err)
					if errors.Is(err, nftables.ErrZapretProbeRecovery) {
						return nil, broker.Failure(broker.CodeRecoveryRequired, "probe Apply outcome requires reconciliation")
					}
					return nil, broker.Failure(broker.CodeValidation, "probe Apply did not qualify")
				}
				return seal, nil
			}
			seal, _, err := change.load(zapretApply)
			if err != nil {
				return nil, broker.Failure(broker.CodeRecoveryRequired, "probe before-image authority missing")
			}
			if verb == zapretRollback {
				err = probe.Rollback(ctx, seal)
			} else {
				err = probe.Verify(ctx, seal)
			}
			if err != nil {
				t.Logf("Zapret probe %s: %v", verb, err)
				return nil, broker.Failure(broker.CodeRecoveryRequired, "probe observation did not match the exact Apply authority")
			}
			return seal, nil
		}
		if err := registry.Register(verb, def); err != nil {
			t.Fatal(err)
		}
	}
	server, err := broker.NewServer(registry, journal, attestor, boot)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture uses a private socket, never the installed broker's path.
	path := filepath.Join(t.TempDir(), "probe.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	serveCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- server.Serve(serveCtx, listener, broker.RolePanel) }()
	change.stop = sync.OnceFunc(func() {
		cancel()
		_ = listener.Close()
		if err := <-done; err != nil {
			t.Errorf("qualification broker listener: %v", err)
		}
		server.ShutdownConnections()
		server.WaitConnections()
	})
	t.Cleanup(change.stop)
	change.client = broker.NewClient(broker.RolePanel)
	change.client.SocketPath = path
	change.client.Verify = verifyZapretProbeListener
	return change
}

func verifyZapretProbeListener(conn net.Conn, _ string) error {
	socket, ok := conn.(*net.UnixConn)
	if !ok {
		return errors.New("qualification socket is not Unix")
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		return err
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) { cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil {
		return err
	}
	if credErr != nil || cred == nil || cred.Uid != 0 || cred.Pid != int32(os.Getpid()) {
		return errors.New("qualification listener process does not match")
	}
	return nil
}

func (change zapretProbeChange) load(verb broker.Verb) (nftables.ZapretProbeSeal, *broker.Receipt, error) {
	var seal nftables.ZapretProbeSeal
	response, err := change.journal.CompletedMutation(zapretChangeID, verb, zapretFence)
	if err != nil || response.Receipt == nil || !response.OK {
		return seal, nil, errors.Join(errors.New("probe completion unavailable"), err)
	}
	err = broker.DecodeRawPayload(response.Payload, &seal)
	return seal, response.Receipt, err
}

func (change zapretProbeChange) invoke(ctx context.Context, verb broker.Verb, sequence uint64) (*broker.Receipt, error) {
	return change.client.Invoke(ctx, broker.Call{Verb: verb, OperationID: zapretChangeID,
		IdempotencyKey: fmt.Sprintf("probe-%d", sequence), Timeout: 10 * time.Second,
		Payload: zapretProbePayload{PlanRevision: change.revision},
		Fence:   broker.Fence{Resource: zapretFence, Sequence: sequence, Token: change.revision}}, nil)
}

func (change zapretProbeChange) call(ctx context.Context, verb broker.Verb, sequence uint64) error {
	_, err := change.invoke(ctx, verb, sequence)
	return err
}

func (change zapretProbeChange) require(t *testing.T, ctx context.Context, verb broker.Verb, sequence uint64) *broker.Receipt {
	t.Helper()
	receipt, err := change.invoke(ctx, verb, sequence)
	if err != nil || receipt == nil || receipt.Outcome != "succeeded" || receipt.Verb != verb || receipt.FenceSequence != sequence {
		t.Fatalf("%s did not commit its exact broker receipt: %+v (%v)", verb, receipt, err)
	}
	return receipt
}

func probeBrokerFailure(err error, code broker.ErrorCode) bool {
	var public *broker.PublicError
	return errors.As(err, &public) && public.Code == code
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
	zapretExchange(t, conn)
}

func zapretExchange(t *testing.T, conn net.Conn) {
	t.Helper()
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

func zapretHandoffHandle(t *testing.T, raw []byte, counter string) uint64 {
	t.Helper()
	var listing struct {
		NFTables []struct {
			Rule *struct {
				Handle uint64
				Expr   []struct{ Counter json.RawMessage }
			}
		}
	}
	if err := json.Unmarshal(raw, &listing); err != nil {
		t.Fatal(err)
	}
	for _, item := range listing.NFTables {
		if item.Rule == nil {
			continue
		}
		for _, expr := range item.Rule.Expr {
			if string(expr.Counter) == fmt.Sprintf("%q", counter) && item.Rule.Handle != 0 {
				return item.Rule.Handle
			}
		}
	}
	t.Fatalf("handoff rule for %s not found: %s", counter, raw)
	return 0
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
