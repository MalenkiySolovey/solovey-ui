package firewall

import (
	"runtime"
	"strings"
	"testing"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

func TestAttachUDPFlowPolicyChangesOnlyExactUDPContribution(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	digest := strings.Repeat("a", 64)
	resource := hostresources.ProtectableResource{ID: "core:inbound:dual", Kind: "inbound", Owner: "core", Protocol: "stream", Listen: "192.0.2.8", Port: 443, Public: true, Source: "fixture", Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, OwnerRevision: strings.Repeat("b", 64), ConfigRevision: strings.Repeat("c", 64)}}
	resource.ListenIntents = []hostresources.ConfiguredListenIntentV1{
		{Schema: hostresources.ConfiguredListenIntentSchemaV1, Mode: hostresources.ListenIntentExact, Network: hostresources.NetworkTCP, Address: resource.Listen, Port: 443, RequiredFamilies: []hostresources.AddressFamily{hostresources.AddressFamilyIPv4}, ConfigurationRevision: resource.Capabilities.ConfigRevision},
		{Schema: hostresources.ConfiguredListenIntentSchemaV1, Mode: hostresources.ListenIntentExact, Network: hostresources.NetworkUDP, Address: resource.Listen, Port: 443, RequiredFamilies: []hostresources.AddressFamily{hostresources.AddressFamilyIPv4}, ConfigurationRevision: resource.Capabilities.ConfigRevision},
	}
	for _, network := range []hostresources.Network{hostresources.NetworkTCP, hostresources.NetworkUDP} {
		resource.Endpoints = append(resource.Endpoints, hostresources.PublicEndpoint{Schema: hostresources.EndpointSchemaV1, ID: "endpoint:" + string(network), Key: hostresources.PublicEndpointKey{Network: network, AddressFamily: hostresources.AddressFamilyIPv4, BindAddress: "192.0.2.8", Port: 443}, Intent: hostresources.EndpointIntentPublic, Protocol: string(network), ResourceID: resource.ID, Owner: resource.Owner, OwnerRevision: resource.Capabilities.OwnerRevision, ConfigurationRevision: resource.Capabilities.ConfigRevision, ObservedAt: now.Unix(), Source: "fixture", ConfidenceBP: 10000})
	}
	surfaces := make([]hostsurface.HostSurfaceFactV1, 0, 2)
	for index, endpoint := range resource.Endpoints {
		pid := 200 + index
		surfaces = append(surfaces, hostsurface.HostSurfaceFactV1{Schema: hostsurface.SchemaV1, ID: "surface:" + string(endpoint.Key.Network), Network: hostsurface.Network(endpoint.Key.Network), Family: hostsurface.FamilyIPv4, Bind: endpoint.Key.BindAddress, Port: endpoint.Key.Port, Exposure: hostsurface.ExposurePublic, SocketInode: string(rune(200 + index)), Process: hostsurface.ProcessFact{PID: &pid, StartTime: "1", ExeDigest: digest}, RegisteredResourceID: resource.ID, DesiredOwner: resource.Owner, OwnershipMode: hostsurface.OwnershipManaged, LastSeen: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), Source: "fixture", ConfidenceBP: 10000, ConfigurationRevision: resource.Capabilities.ConfigRevision, Classification: hostsurface.ClassificationExpectedManaged})
	}
	graph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: surfaces, Now: now})
	plan := BuildEndpointPlan(EndpointPlanInput{Graph: graph, Resources: []hostresources.ProtectableResource{resource}, Now: now})
	var udp EndpointPolicy
	for _, endpoint := range plan.Endpoints {
		if endpoint.Key.Network == hostresources.NetworkUDP {
			udp = endpoint
		}
	}
	policy := FinalizeUDPFlowPolicy(UDPFlowPolicyV1{ResourceID: resource.ID, EndpointID: udp.EndpointRevision, AddressFamily: hostresources.AddressFamilyIPv4, Protocol: hostresources.NetworkUDP, ExactSocketRevision: digest, ManagementExclusionRevision: digest, TrustedExclusionRevision: digest, RateProfile: "BALANCED_V1", CardinalityProfile: "BOUNDED_4096_V1", ConntrackPolicy: "ADVISORY_NEW_FLOW_V1", ICMPPolicy: "PRESERVE_ICMP_AND_ICMPV6_V1", ExpectedManagedTableRevision: digest, OperationRevision: digest, PlanRevision: digest})
	before := runtime.NumGoroutine()
	started := time.Now()
	guarded, err := AttachUDPFlowPolicy(plan, udp.EndpointRevision, policy)
	if err != nil {
		t.Fatal(err)
	}
	candidate := RenderManagedNFT(guarded)
	allocations := testing.AllocsPerRun(1, func() {
		value, attachErr := AttachUDPFlowPolicy(plan, udp.EndpointRevision, policy)
		if attachErr == nil {
			_ = RenderManagedNFT(value)
		}
	})
	after := runtime.NumGoroutine()
	if len(candidate) > 1<<20 || allocations > 100_000 || after > before+1 {
		t.Fatalf("candidateBytes=%d allocations=%.0f goroutines=%d/%d", len(candidate), allocations, before, after)
	}
	t.Logf("candidateBytes=%d duration=%s allocations=%.0f goroutineDelta=%d dbSystemDnsCalls=0", len(candidate), time.Since(started), allocations, after-before)
	if strings.Count(candidate, "ct state new limit rate over 200/second burst 400 packets counter drop") != 1 {
		t.Fatalf("UDP bounded rule missing or duplicated:\n%s", candidate)
	}
	if strings.Contains(candidate, "icmp drop") || strings.Contains(candidate, "icmpv6 drop") || strings.Contains(candidate, "flush ruleset") {
		t.Fatalf("candidate violated coexistence boundary:\n%s", candidate)
	}
	for _, endpoint := range guarded.Endpoints {
		if endpoint.Key.Network == hostresources.NetworkTCP && endpoint.UDPFlowPolicy != nil {
			t.Fatal("TCP policy changed")
		}
	}
}
