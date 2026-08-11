package udpguard

import (
	"net/netip"
	"sort"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

type PlannerInput struct {
	Capabilities                []hostresources.InboundTransportCapabilityV2
	Graph                       protectionresources.SocketOwnershipGraph
	ManagementExclusionRevision string
	TrustedExclusionRevision    string
	FirewallBaselineRevision    string
	FirewallAuthorityActive     bool
	ProbeCapabilities           map[string]componenthealth.ProtocolProbeCapabilityV1
	Now                         time.Time
}

func BuildStatus(input PlannerInput) StatusV1 {
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	status := StatusV1{Schema: UDPStatusSchemaV1, GeneratedAt: now.Unix(), Experimental: true, DefaultApplyEnabled: false,
		Capabilities: []CapabilityStatusV1{}, Plans: []UDPDirectGuardPlanV1{}}
	nodes := make(map[string]protectionresources.SocketGraphNode, len(input.Graph.Nodes))
	for _, node := range input.Graph.Nodes {
		nodes[node.ResourceID] = node
	}
	facts := append([]hostresources.InboundTransportCapabilityV2(nil), input.Capabilities...)
	sort.Slice(facts, func(i, j int) bool { return facts[i].ResourceID < facts[j].ResourceID })
	for _, fact := range facts {
		capStatus := CapabilityStatusV1{ResourceID: fact.ResourceID, InboundType: fact.InboundType, StrategyClass: fact.StrategyClass,
			ShippingStatus:    protocolShippingStatus(fact),
			EffectiveNetworks: append([]hostresources.Network(nil), fact.EffectiveNetworks...), Configured: true,
			DependentAssociation: fact.DependentAssociation, BuildFeatureState: fact.BuildFeature.State,
			AuthenticationPresent: fact.AuthenticationPresent, TLSPresent: fact.TLSPresent,
			ProtocolOwnedZeroRTT: fact.ProtocolOwnedZeroRTT, ProtocolOwnedMigration: fact.ProtocolOwnedMigration,
			ActualState: StateBlocked, ApplyGate: ApplyGateBlocked, ReasonCodes: append([]string(nil), fact.ReasonCodes...)}
		if fact.StrategyClass == hostresources.TransportUnsupported {
			capStatus.ActualState = StateUnsupported
		}
		if fact.StrategyClass == hostresources.TransportExternalManaged {
			capStatus.ActualState = StateExternalManaged
		}
		node, ok := nodes[fact.ResourceID]
		if !ok {
			capStatus.ReasonCodes = reasons(append(capStatus.ReasonCodes, "BLOCKED_MISSING_SOCKET_CLAIM"))
			status.Capabilities = append(status.Capabilities, capStatus)
			continue
		}
		for _, observed := range node.ObservedClaims {
			if observed.Key.Network != hostresources.NetworkUDP || !exactAddress(observed.Key.BindAddress, observed.Key.AddressFamily) || observed.Key.Port == 0 {
				continue
			}
			capStatus.Observed = true
			plan := buildPlan(fact, node, observed, input, now)
			status.Plans = append(status.Plans, plan)
			if plan.ApplyGate != ApplyGateBlocked {
				capStatus.ActualState, capStatus.ApplyGate = StateNotApplied, plan.ApplyGate
			}
			capStatus.ReasonCodes = reasons(append(capStatus.ReasonCodes, plan.BlockCodes...))
		}
		if !capStatus.Observed {
			capStatus.ReasonCodes = reasons(append(capStatus.ReasonCodes, "BLOCKED_MISSING_SOCKET_CLAIM"))
		}
		status.Capabilities = append(status.Capabilities, capStatus)
	}
	sort.Slice(status.Plans, func(i, j int) bool { return status.Plans[i].EndpointID < status.Plans[j].EndpointID })
	return status
}

func protocolShippingStatus(fact hostresources.InboundTransportCapabilityV2) string {
	switch fact.InboundType {
	case "shadowsocks":
		if fact.StrategyClass == hostresources.TransportPlainUDP || fact.StrategyClass == hostresources.TransportTCPUDPDual {
			return "SHIP"
		}
	case "direct", "hysteria", "hysteria2", "tuic", "naive", "vless", "vmess":
		return "INSPECTION_ONLY"
	case "h2", "socks", "mixed", "tun", "redirect", "tproxy":
		return "NOT_SHIPPED"
	}
	if fact.StrategyClass == hostresources.TransportUnsupported || fact.StrategyClass == hostresources.TransportProxyUDPAssociation || fact.StrategyClass == hostresources.TransportInterception || fact.StrategyClass == hostresources.TransportLocalProxy {
		return "NOT_SHIPPED"
	}
	return "INSPECTION_ONLY"
}

func buildPlan(fact hostresources.InboundTransportCapabilityV2, node protectionresources.SocketGraphNode, observed protectionresources.SocketClaim, input PlannerInput, now time.Time) UDPDirectGuardPlanV1 {
	expires := fact.ExpiresAt
	if observed.ExpiresAt > 0 && observed.ExpiresAt < expires {
		expires = observed.ExpiresAt
	}
	exposure := "PUBLIC"
	if address, err := netip.ParseAddr(observed.Key.BindAddress); err == nil {
		if address.IsLoopback() {
			exposure = "LOCAL"
		} else if address.IsPrivate() {
			exposure = "PRIVATE"
		}
	}
	probeCapability := input.ProbeCapabilities[fact.ResourceID+"|"+observed.ID]
	healthRevision := probeCapability.Revision
	if !digest(healthRevision) {
		healthRevision = missingRevision("health", fact.ResourceID, observed.ID)
	}
	managementRevision := input.ManagementExclusionRevision
	if !digest(managementRevision) {
		managementRevision = missingRevision("management", fact.ResourceID, observed.ID)
	}
	trustedRevision := input.TrustedExclusionRevision
	if !digest(trustedRevision) {
		trustedRevision = missingRevision("trusted", fact.ResourceID, observed.ID)
	}
	firewallRevision := input.FirewallBaselineRevision
	if !digest(firewallRevision) {
		firewallRevision = missingRevision("firewall", fact.ResourceID, observed.ID)
	}
	claim := UDPConfiguredSocketClaimV1{ResourceID: fact.ResourceID, EndpointID: observed.ID, ProviderID: fact.ProviderID,
		ProviderRevision: fact.Revision, Protocol: hostresources.NetworkUDP, AddressFamily: observed.Key.AddressFamily,
		ConfiguredBind: observed.Key.BindAddress, Exposure: exposure, Port: observed.Key.Port,
		SocketIntentRevision: fact.SocketIntentRevision, ConfigurationRevision: fact.ConfigurationRevision,
		RuntimeGenerationRevision: fact.RuntimeGenerationRevision, OwnerRevision: observed.OwnerRevision,
		ListenerObservationRevision: observed.OwnerObservationRevision, ManagementExclusionRevision: managementRevision,
		HealthRevision: healthRevision, ObservedAt: observed.ObservedAt, ExpiresAt: expires, ReasonCodes: append([]string(nil), observed.ReasonCodes...)}
	claim.ClaimRevision = claimRevision(claim)
	blocks := append([]string(nil), fact.ReasonCodes...)
	if !fact.ActionableDirectUDPSocket {
		blocks = append(blocks, "BLOCKED_MISSING_CAPABILITY")
	}
	if node.ApplyBlocked || observed.Ambiguous || observed.Stale || observed.Truncated {
		blocks = append(blocks, "BLOCKED_SOCKET_OWNERSHIP")
	}
	if !probeCapability.Available || probeCapability.ProtocolClass != fact.StrategyClass || probeCapability.ResourceID != fact.ResourceID || probeCapability.EndpointID != observed.ID {
		blocks = append(blocks, "BLOCKED_MISSING_HEALTH")
	}
	if !digest(input.ManagementExclusionRevision) {
		blocks = append(blocks, "BLOCKED_MANAGEMENT_PRESERVATION")
	}
	if !digest(input.FirewallBaselineRevision) {
		blocks = append(blocks, "BLOCKED_FIREWALL_BASELINE")
	}
	if !input.FirewallAuthorityActive {
		blocks = append(blocks, "BLOCKED_FIREWALL_BASELINE_NOT_APPLIED")
	}
	if observed.Key.Port == 53 {
		blocks = append(blocks, "BLOCKED_MISSING_SERVICE_PROVIDER", "NOT_SHIPPED_GENERIC_DNS_GUARD")
	}
	blocks = reasons(blocks)
	plan := UDPDirectGuardPlanV1{Schema: UDPDirectGuardPlanSchemaV1, CreatedAt: now.Unix(), ExpiresAt: expires,
		ResourceID: fact.ResourceID, EndpointID: observed.ID, CapabilityRevision: fact.Revision,
		BuildFeatureRevision: fact.BuildFeature.Revision, Claim: claim, StrategyClass: fact.StrategyClass,
		DesiredPolicy: "UDP_DIRECT_GUARDED", SelectedStrategy: "UDP_DIRECT_GUARDED", ActualState: StateNotApplied,
		ApplyGate: ApplyGateExperimentalOff, FirewallBaselineRevision: firewallRevision,
		ManagementExclusionRevision: managementRevision, HealthRevision: healthRevision, BlockCodes: blocks,
		WarningCodes: []string{"EXPERIMENTAL_KERNEL_GUARD_ONLY"}}
	if fact.StrategyClass == hostresources.TransportQUICNative || fact.StrategyClass == hostresources.TransportQUICV2Ray || fact.InboundType == "naive" {
		plan.WarningCodes = append(plan.WarningCodes, "QUIC_REMAINS_CORE_TERMINATED")
	}
	if len(blocks) != 0 {
		plan.ApplyGate = ApplyGateBlocked
	}
	plan.SafetyRevision = hostresources.Revision(struct{ Capability, Claim, Health, Management, Firewall string }{fact.Revision, claim.ClaimRevision, healthRevision, managementRevision, firewallRevision})
	operationRevision := hostresources.Revision(struct{ Resource, Endpoint, Safety string }{fact.ResourceID, observed.ID, plan.SafetyRevision})
	plan.FlowPolicy = protectionfirewall.FinalizeUDPFlowPolicy(protectionfirewall.UDPFlowPolicyV1{ResourceID: fact.ResourceID, EndpointID: observed.ID,
		AddressFamily: observed.Key.AddressFamily, Protocol: hostresources.NetworkUDP, ExactSocketRevision: claim.ClaimRevision,
		ManagementExclusionRevision: managementRevision, TrustedExclusionRevision: trustedRevision,
		RateProfile: "BALANCED_V1", CardinalityProfile: "BOUNDED_4096_V1", ConntrackPolicy: "ADVISORY_NEW_FLOW_V1",
		ICMPPolicy: "PRESERVE_ICMP_AND_ICMPV6_V1", ExpectedManagedTableRevision: firewallRevision,
		OperationRevision: operationRevision, PlanRevision: operationRevision})
	plan.PlanDigest = planDigest(plan)
	plan.PlanID = "udp-plan:" + plan.PlanDigest[:32]
	return plan
}

func missingRevision(kind, resourceID, endpointID string) string {
	return hostresources.Revision(struct{ Schema, Kind, ResourceID, EndpointID string }{
		Schema: "solovey-ui/udp-missing-fact/v1", Kind: kind, ResourceID: resourceID, EndpointID: endpointID,
	})
}
