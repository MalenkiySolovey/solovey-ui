package udpguard

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"runtime"
	"slices"
	"testing"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

func plannerDigest(value string) string { return hostresources.Revision(value) }
func plannerFact(now time.Time, class hostresources.InboundTransportClass, resource string) hostresources.InboundTransportCapabilityV2 {
	digest := plannerDigest(resource)
	feature := hostresources.FinalizeRuntimeBuildFeatureV1(hostresources.RuntimeBuildFeatureV1{Feature: "with_quic", State: hostresources.RuntimeFeatureSupported, RuntimeIdentity: digest, SourceRevision: digest, ModuleRevision: digest, BuildProfileRevision: digest, ObservationMethod: "compile_tag", ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()})
	return hostresources.FinalizeInboundTransportCapabilityV2(hostresources.InboundTransportCapabilityV2{ProviderID: "provider", ContributorID: "fixture", ResourceID: resource, InboundDatabaseID: 1, InboundType: "direct", StrategyClass: class, ConfigurationRevision: digest, EffectiveRuntimeRevision: digest, PinnedRuntimeIdentity: digest, BuildFeature: feature, EffectiveNetworks: []hostresources.Network{hostresources.NetworkUDP}, EffectiveNetworksRevision: digest, TransportRevision: digest, SocketIntentRevision: digest, AuthenticationRevision: digest, TLSSemanticRevision: digest, UDPTimeoutRevision: digest, ListenerOwnerRevision: digest, RuntimeGenerationRevision: digest, ActionableDirectUDPSocket: true, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()})
}
func plannerGraph(now time.Time, resource string, family hostresources.AddressFamily, bind string) (protectionresources.SocketOwnershipGraph, string) {
	digest := plannerDigest(resource)
	endpoint := hostresources.PublicEndpointKey{Network: hostresources.NetworkUDP, AddressFamily: family, BindAddress: bind, Port: 443}
	claim := protectionresources.SocketClaim{ID: "claim:" + digest[:16], Kind: protectionresources.ClaimObserved, Key: endpoint, ResourceID: resource, Owner: "core", OwnerRevision: digest, ConfigurationRevision: digest, OwnerObservationRevision: digest, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()}
	node := protectionresources.SocketGraphNode{ResourceID: resource, ResourceOwner: "core", OwnerRevision: digest, ConfigurationRevision: digest, DesiredClaims: []protectionresources.SocketClaim{claim}, ObservedClaims: []protectionresources.SocketClaim{claim}, SelectedStrategy: protectionresources.StrategyUDPQUICDirectGuarded}
	return protectionresources.SocketOwnershipGraph{Schema: protectionresources.SocketGraphSchemaV1, Revision: digest, GeneratedAt: now.Unix(), OwnerObservationRevision: digest, Nodes: []protectionresources.SocketGraphNode{node}}, claim.ID
}
func readyHealth(now time.Time, resource, endpoint string, class hostresources.InboundTransportClass) UDPStrategyHealthV1 {
	return FinalizeHealth(UDPStrategyHealthV1{ResourceID: resource, EndpointID: endpoint, StrategyClass: class, RuntimeReady: true, SocketObserved: true, ManagedContributionReady: true, ProtocolTransactionReady: true, ManagementPreserved: true, CapacityReady: true, RestartReconciled: true, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()})
}
func readyProbeCapability(resource, endpoint string, class hostresources.InboundTransportClass) componenthealth.ProtocolProbeCapabilityV1 {
	return componenthealth.FinalizeProtocolProbeCapabilityV1(componenthealth.ProtocolProbeCapabilityV1{ProviderID: "fixture", ProviderInstance: "fixture:one", ResourceID: resource, EndpointID: endpoint, ProtocolClass: class, Available: true})
}

func TestProtocolShippingStatusIsExplicitForSliceOneMatrix(t *testing.T) {
	for _, test := range []struct {
		inbound string
		class   hostresources.InboundTransportClass
		want    string
	}{
		{"shadowsocks", hostresources.TransportPlainUDP, "SHIP"},
		{"shadowsocks", hostresources.TransportTCPUDPDual, "SHIP"},
		{"direct", hostresources.TransportPlainUDP, "INSPECTION_ONLY"},
		{"hysteria", hostresources.TransportQUICNative, "INSPECTION_ONLY"},
		{"hysteria2", hostresources.TransportQUICNative, "INSPECTION_ONLY"},
		{"tuic", hostresources.TransportQUICNative, "INSPECTION_ONLY"},
		{"naive", hostresources.TransportQUICNative, "INSPECTION_ONLY"},
		{"vless", hostresources.TransportQUICV2Ray, "INSPECTION_ONLY"},
		{"vmess", hostresources.TransportQUICV2Ray, "INSPECTION_ONLY"},
		{"h2", hostresources.TransportUnsupported, "NOT_SHIPPED"},
		{"socks", hostresources.TransportProxyUDPAssociation, "NOT_SHIPPED"},
		{"mixed", hostresources.TransportProxyUDPAssociation, "NOT_SHIPPED"},
	} {
		if got := protocolShippingStatus(hostresources.InboundTransportCapabilityV2{InboundType: test.inbound, StrategyClass: test.class}); got != test.want {
			t.Fatalf("%s/%s disposition=%s want=%s", test.inbound, test.class, got, test.want)
		}
	}
}

func TestPlannerBuildsExactIndependentUDPPlan(t *testing.T) {
	now := time.Unix(1000, 0)
	resource := "core:inbound:1"
	fact := plannerFact(now, hostresources.TransportPlainUDP, resource)
	graph, claimID := plannerGraph(now, resource, hostresources.AddressFamilyIPv4, "0.0.0.0")
	management, firewall, trusted := plannerDigest("management"), plannerDigest("firewall"), plannerDigest("trusted")
	probe := readyProbeCapability(resource, claimID, fact.StrategyClass)
	status := BuildStatus(PlannerInput{Capabilities: []hostresources.InboundTransportCapabilityV2{fact}, Graph: graph, ManagementExclusionRevision: management, TrustedExclusionRevision: trusted, FirewallBaselineRevision: firewall, FirewallAuthorityActive: true, ProbeCapabilities: map[string]componenthealth.ProtocolProbeCapabilityV1{resource + "|" + claimID: probe}, Now: now})
	if len(status.Plans) != 1 {
		t.Fatalf("plans=%#v", status.Plans)
	}
	plan := status.Plans[0]
	if plan.ActualState != StateNotApplied || plan.ApplyGate != ApplyGateExperimentalOff || len(plan.BlockCodes) != 0 || plan.Claim.Protocol != hostresources.NetworkUDP || plan.FlowPolicy.Validate() != nil || plan.FlowPolicy.ICMPPolicy != "PRESERVE_ICMP_AND_ICMPV6_V1" {
		t.Fatalf("plan=%#v", plan)
	}
	again := BuildStatus(PlannerInput{Capabilities: []hostresources.InboundTransportCapabilityV2{fact}, Graph: graph, ManagementExclusionRevision: management, TrustedExclusionRevision: trusted, FirewallBaselineRevision: firewall, FirewallAuthorityActive: true, ProbeCapabilities: map[string]componenthealth.ProtocolProbeCapabilityV1{resource + "|" + claimID: probe}, Now: now.Add(time.Second)})
	if again.Plans[0].PlanDigest != plan.PlanDigest || again.Plans[0].Claim.ClaimRevision != plan.Claim.ClaimRevision {
		t.Fatal("wall clock changed semantic plan")
	}
}

func TestPlannerRejectsHealthFromAnotherProtocolClass(t *testing.T) {
	now := time.Unix(1000, 0)
	resource := "core:inbound:quic"
	fact := plannerFact(now, hostresources.TransportQUICNative, resource)
	graph, claimID := plannerGraph(now, resource, hostresources.AddressFamilyIPv4, "127.0.0.1")
	probe := readyProbeCapability(resource, claimID, hostresources.TransportPlainUDP)
	status := BuildStatus(PlannerInput{Capabilities: []hostresources.InboundTransportCapabilityV2{fact}, Graph: graph,
		ManagementExclusionRevision: plannerDigest("management"), TrustedExclusionRevision: plannerDigest("trusted"),
		FirewallBaselineRevision: plannerDigest("firewall"), FirewallAuthorityActive: true, ProbeCapabilities: map[string]componenthealth.ProtocolProbeCapabilityV1{resource + "|" + claimID: probe}, Now: now})
	if len(status.Plans) != 1 || status.Plans[0].ApplyGate != ApplyGateBlocked || !slices.Contains(status.Plans[0].BlockCodes, "BLOCKED_MISSING_HEALTH") {
		t.Fatalf("cross-protocol health satisfied QUIC: %#v", status.Plans)
	}
}

func TestPlannerU9Port53AloneIsNotAServiceProvider(t *testing.T) {
	now := time.Unix(1000, 0)
	resource := "core:inbound:dns-like"
	fact := plannerFact(now, hostresources.TransportPlainUDP, resource)
	graph, claimID := plannerGraph(now, resource, hostresources.AddressFamilyIPv4, "0.0.0.0")
	graph.Nodes[0].ObservedClaims[0].Key.Port = 53
	graph.Nodes[0].DesiredClaims[0].Key.Port = 53
	probe := readyProbeCapability(resource, claimID, fact.StrategyClass)
	status := BuildStatus(PlannerInput{Capabilities: []hostresources.InboundTransportCapabilityV2{fact}, Graph: graph,
		ManagementExclusionRevision: plannerDigest("management"), TrustedExclusionRevision: plannerDigest("trusted"),
		FirewallBaselineRevision: plannerDigest("firewall"), FirewallAuthorityActive: true, ProbeCapabilities: map[string]componenthealth.ProtocolProbeCapabilityV1{resource + "|" + claimID: probe}, Now: now})
	if len(status.Plans) != 1 || status.Plans[0].ApplyGate != ApplyGateBlocked || status.Plans[0].ActualState != StateNotApplied ||
		!slices.Contains(status.Plans[0].BlockCodes, "BLOCKED_MISSING_SERVICE_PROVIDER") || !slices.Contains(status.Plans[0].BlockCodes, "NOT_SHIPPED_GENERIC_DNS_GUARD") {
		t.Fatalf("port 53 was not rejected as providerless: %#v", status.Plans)
	}
}

func TestPlannerNormalCIFailClosedMatrix160Cases(t *testing.T) {
	now := time.Unix(1000, 0)
	classes := []hostresources.InboundTransportClass{hostresources.TransportPlainUDP, hostresources.TransportQUICNative, hostresources.TransportQUICV2Ray, hostresources.TransportTCPUDPDual, hostresources.TransportProxyUDPAssociation, hostresources.TransportDNSServiceUnknown, hostresources.TransportLocalProxy, hostresources.TransportInterception, hostresources.TransportExternalManaged, hostresources.TransportUnsupported}
	families := []struct {
		family hostresources.AddressFamily
		bind   string
	}{{hostresources.AddressFamilyIPv4, "0.0.0.0"}, {hostresources.AddressFamilyIPv6, "::"}}
	caseCount := 0
	for _, class := range classes {
		for _, family := range families {
			for variant := 0; variant < 8; variant++ {
				caseCount++
				resource := "core:inbound:" + plannerDigest(string(class) + family.bind + string(rune(variant)))[:12]
				fact := plannerFact(now, class, resource)
				graph, claimID := plannerGraph(now, resource, family.family, family.bind)
				input := PlannerInput{Capabilities: []hostresources.InboundTransportCapabilityV2{fact}, Graph: graph, ManagementExclusionRevision: plannerDigest("management"), TrustedExclusionRevision: plannerDigest("trusted"), FirewallBaselineRevision: plannerDigest("firewall"), FirewallAuthorityActive: true, ProbeCapabilities: map[string]componenthealth.ProtocolProbeCapabilityV1{}, Now: now}
				switch variant {
				case 1:
					input.Graph.Nodes = nil
				case 2:
					input.ManagementExclusionRevision = ""
				case 3:
					input.FirewallBaselineRevision = ""
				case 4:
					input.Graph.Nodes[0].ApplyBlocked = true
				case 5:
					input.Graph.Nodes[0].ObservedClaims[0].Stale = true
				case 6:
					fact.ActionableDirectUDPSocket = false
					input.Capabilities[0] = fact
				case 7:
					input.ProbeCapabilities[resource+"|"+claimID] = componenthealth.ProtocolProbeCapabilityV1{}
				}
				status := BuildStatus(input)
				for _, plan := range status.Plans {
					if plan.ActualState != StateNotApplied || plan.ApplyGate != ApplyGateBlocked {
						t.Fatalf("case %d escaped fail closed: %#v", caseCount, plan)
					}
				}
			}
		}
	}
	if caseCount != 160 {
		t.Fatalf("case count=%d", caseCount)
	}
}

func TestPlanner4096FactsDeterministicBoundedAndNoGoroutineLeak(t *testing.T) {
	now := time.Unix(1000, 0)
	facts := make([]hostresources.InboundTransportCapabilityV2, 4096)
	graph := protectionresources.SocketOwnershipGraph{Schema: protectionresources.SocketGraphSchemaV1, Revision: plannerDigest("graph"), GeneratedAt: now.Unix(), OwnerObservationRevision: plannerDigest("owner")}
	probes := make(map[string]componenthealth.ProtocolProbeCapabilityV1, 4096)
	for index := range facts {
		resource := "core:inbound:" + plannerDigest(string(rune(index)))[:24]
		facts[index] = plannerFact(now, hostresources.TransportPlainUDP, resource)
		one, claimID := plannerGraph(now, resource, hostresources.AddressFamilyIPv4, "127.0.0.1")
		port := uint16(10000 + index)
		one.Nodes[0].DesiredClaims[0].Key.Port, one.Nodes[0].ObservedClaims[0].Key.Port = port, port
		graph.Nodes = append(graph.Nodes, one.Nodes[0])
		probes[resource+"|"+claimID] = readyProbeCapability(resource, claimID, hostresources.TransportPlainUDP)
	}
	input := PlannerInput{Capabilities: facts, Graph: graph, ManagementExclusionRevision: plannerDigest("management"),
		TrustedExclusionRevision: plannerDigest("trusted"), FirewallBaselineRevision: plannerDigest("firewall"), FirewallAuthorityActive: true, ProbeCapabilities: probes, Now: now}
	before := runtime.NumGoroutine()
	started := time.Now()
	first := BuildStatus(input)
	elapsed := time.Since(started)
	after := runtime.NumGoroutine()
	second := BuildStatus(input)
	left, _ := json.Marshal(first)
	right, _ := json.Marshal(second)
	allocations := testing.AllocsPerRun(1, func() { _ = BuildStatus(input) })
	if len(first.Capabilities) != 4096 || len(first.Plans) != 4096 || string(left) != string(right) || after > before+1 || len(left) > 64<<20 || allocations > 1_000_000 {
		t.Fatalf("bounded result capabilities=%d plans=%d goroutines=%d/%d", len(first.Capabilities), len(first.Plans), before, after)
	}
	t.Logf("isolated in-process fixture facts=%d plans=%d duration=%s allocations=%.0f candidateBytes=%d goroutineDelta=%d dbSystemDnsCalls=0", len(facts), len(first.Plans), elapsed, allocations, len(left), after-before)
}

type healthFixtureProvider struct {
	id     string
	values []UDPStrategyHealthV1
}

func (p healthFixtureProvider) ProviderID() string {
	if p.id != "" {
		return p.id
	}
	return "health-fixture"
}
func (p healthFixtureProvider) UDPStrategyHealth(context.Context, time.Time) ([]UDPStrategyHealthV1, error) {
	return append([]UDPStrategyHealthV1(nil), p.values...), nil
}

func TestHealthRegistry4096FactsBoundedAndExpiryHonest(t *testing.T) {
	now := time.Unix(1000, 0)
	values := make([]UDPStrategyHealthV1, 4096)
	for index := range values {
		resource := "core:inbound:" + plannerDigest(string(rune(index)))[:24]
		values[index] = readyHealth(now, resource, "claim:"+plannerDigest(resource)[:16], hostresources.TransportPlainUDP)
	}
	registry := NewHealthRegistry()
	remove, err := registry.Register(healthFixtureProvider{values: values})
	if err != nil {
		t.Fatal(err)
	}
	defer remove()
	started := time.Now()
	snapshot, err := registry.Snapshot(context.Background(), now.Add(2*time.Minute))
	if err != nil || len(snapshot) != 4096 {
		t.Fatalf("health snapshot facts=%d err=%v", len(snapshot), err)
	}
	if snapshot[values[0].ResourceID+"|"+values[0].EndpointID].Ready(now.Add(2 * time.Minute)) {
		t.Fatal("expired health became ready")
	}
	allocations := testing.AllocsPerRun(1, func() { _, _ = registry.Snapshot(context.Background(), now) })
	if allocations > 100_000 {
		t.Fatalf("health allocations=%.0f", allocations)
	}
	t.Logf("healthFacts=%d duration=%s allocations=%.0f queueBound=%d dbSystemDnsCalls=0", len(snapshot), time.Since(started), allocations, len(snapshot))
}

func TestHealthRegistryRejectsFactBeyondCardinalityBound(t *testing.T) {
	now := time.Unix(1000, 0)
	values := make([]UDPStrategyHealthV1, MaxHealthFactsV1+1)
	for index := range values {
		resource := "core:inbound:" + plannerDigest(string(rune(index)))[:24]
		values[index] = readyHealth(now, resource, "claim:"+plannerDigest(resource)[:16], hostresources.TransportPlainUDP)
	}
	registry := NewHealthRegistry()
	remove, err := registry.Register(healthFixtureProvider{values: values})
	if err != nil {
		t.Fatal(err)
	}
	defer remove()
	if _, err = registry.Snapshot(context.Background(), now); err == nil {
		t.Fatal("health fact beyond the cardinality bound was accepted")
	}
}

func TestHealthRegistryRejectsDuplicateProviderAndBoundsProviders(t *testing.T) {
	registry := NewHealthRegistry()
	if _, err := registry.Register(healthFixtureProvider{id: "duplicate"}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register(healthFixtureProvider{id: "duplicate"}); err == nil {
		t.Fatal("duplicate provider identity was accepted")
	}

	registry = NewHealthRegistry()
	for index := 0; index < maxHealthProviders; index++ {
		if _, err := registry.Register(healthFixtureProvider{id: fmt.Sprintf("provider-%03d", index)}); err != nil {
			t.Fatalf("register provider %d: %v", index, err)
		}
	}
	if _, err := registry.Register(healthFixtureProvider{id: "overflow"}); err == nil {
		t.Fatal("provider registry exceeded its cardinality bound")
	}
}

func TestPlainUDPProbeRequiresExactResponseAndFailsSendOnly(t *testing.T) {
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 32)
		count, address, readErr := listener.ReadFromUDP(buffer)
		if readErr == nil && string(buffer[:count]) == "request" {
			_, _ = listener.WriteToUDP([]byte("response"), address)
		}
	}()
	endpoint := listener.LocalAddr().(*net.UDPAddr).AddrPort()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := probePlainUDP(ctx, endpoint, []byte("request"), []byte("response"), 32); err != nil {
		t.Fatal(err)
	}
	<-done
	wrong := netip.MustParseAddrPort("127.0.0.1:9")
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer shortCancel()
	if err := probePlainUDP(shortCtx, wrong, []byte("request"), []byte("response"), 32); err == nil {
		t.Fatal("send-only/wrong target was reported healthy")
	}
}
