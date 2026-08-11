package firewall

import (
	"net/netip"
	"sort"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

type EligibilityKind string

const (
	FirewallBaselineEligibilityKind         EligibilityKind = "FIREWALL_BASELINE_ELIGIBILITY"
	ListenerTopologyMutationEligibilityKind EligibilityKind = "LISTENER_TOPOLOGY_MUTATION_ELIGIBILITY"
)

// FirewallBaselineEligibility is the additive managed-table gate. Exact
// listener ownership is deliberately advisory here; prepare/apply additionally
// require a fresh recovery path and the workflow's typed helper capabilities.
type FirewallBaselineEligibility struct {
	Kind                      EligibilityKind `json:"kind"`
	Revision                  string          `json:"revision"`
	CandidateEligible         bool            `json:"candidateEligible"`
	MutationReady             bool            `json:"mutationReady"`
	EndpointInventoryComplete bool            `json:"endpointInventoryComplete"`
	ManagementPreserved       bool            `json:"managementPreserved"`
	ExactRevisions            bool            `json:"exactRevisions"`
	ManagedTableOnly          bool            `json:"managedTableOnly"`
	NoForeignMutation         bool            `json:"noForeignMutation"`
	ReasonCodes               []string        `json:"reasonCodes,omitempty"`
	MutationReasonCodes       []string        `json:"mutationReasonCodes,omitempty"`
	AdvisoryCodes             []string        `json:"advisoryCodes,omitempty"`
}

// ListenerTopologyMutationEligibility keeps exact owner observations as a hard
// prerequisite for handoff, takeover, fronting and other listener mutations.
type ListenerTopologyMutationEligibility struct {
	Kind                     EligibilityKind `json:"kind"`
	Revision                 string          `json:"revision"`
	Eligible                 bool            `json:"eligible"`
	GraphRevision            string          `json:"graphRevision"`
	OwnerObservationRevision string          `json:"ownerObservationRevision,omitempty"`
	ReasonCodes              []string        `json:"reasonCodes,omitempty"`
}

type firewallBaselineEligibilityBinding struct {
	Kind                      EligibilityKind
	CandidateEligible         bool
	MutationReady             bool
	EndpointInventoryComplete bool
	ManagementPreserved       bool
	ExactRevisions            bool
	ManagedTableOnly          bool
	NoForeignMutation         bool
	ReasonCodes               []string
	MutationReasonCodes       []string
}

func baselineEligibilityBinding(value FirewallBaselineEligibility) firewallBaselineEligibilityBinding {
	return firewallBaselineEligibilityBinding{
		Kind: value.Kind, CandidateEligible: value.CandidateEligible, MutationReady: value.MutationReady,
		EndpointInventoryComplete: value.EndpointInventoryComplete, ManagementPreserved: value.ManagementPreserved,
		ExactRevisions: value.ExactRevisions, ManagedTableOnly: value.ManagedTableOnly, NoForeignMutation: value.NoForeignMutation,
		ReasonCodes: append([]string(nil), value.ReasonCodes...), MutationReasonCodes: append([]string(nil), value.MutationReasonCodes...),
	}
}

func validBaselineEligibility(value FirewallBaselineEligibility) bool {
	return exactHexRevision(value.Revision) && value.Revision == hostresources.Revision(baselineEligibilityBinding(value))
}

func evaluateFirewallBaselineEligibility(resources []hostresources.ProtectableResource, endpoints []EndpointPolicy, management []hostresources.ManagementEndpointV1, recovery []hostresources.RecoveryPathV1, trusted []string, graph protectionresources.SocketOwnershipGraph, requireSSH bool, now time.Time) FirewallBaselineEligibility {
	result := FirewallBaselineEligibility{
		Kind: FirewallBaselineEligibilityKind, CandidateEligible: true, MutationReady: true,
		EndpointInventoryComplete: true, ManagementPreserved: true, ExactRevisions: true,
		ManagedTableOnly: true, NoForeignMutation: true,
	}
	if len(resources) == 0 || len(endpoints) == 0 {
		result.EndpointInventoryComplete = false
		result.ReasonCodes = append(result.ReasonCodes, "endpoint_inventory_incomplete")
	}

	endpointKeys := make(map[string]EndpointPolicy, len(endpoints))
	endpointKeysOnly := make(map[string]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		endpointKeys[endpoint.ResourceID+"\x00"+endpointKeyString(endpoint.Key)] = endpoint
		endpointKeysOnly[endpointKeyString(endpoint.Key)] = struct{}{}
		if endpoint.ResourceID == "" || !exactHexRevision(endpoint.ConfigurationRevision) || !exactHexRevision(endpoint.EndpointRevision) {
			result.ExactRevisions = false
			result.ReasonCodes = append(result.ReasonCodes, "endpoint_revision_incomplete")
		}
	}
	managementKeys := make(map[string]hostresources.ManagementEndpointV1, len(management))
	sshPreserved := false
	for _, endpoint := range management {
		key := hostresources.PublicEndpointKey{
			Network: endpoint.Network, AddressFamily: endpoint.Family, BindAddress: endpoint.Bind, Port: endpoint.Port,
		}
		managementKeys[endpoint.ResourceID+"\x00"+endpointKeyString(key)] = endpoint
		if !managementEndpointUsable(endpoint) {
			result.ManagementPreserved = false
			result.ReasonCodes = append(result.ReasonCodes, "management_endpoint_inventory_incomplete")
		}
		if _, exists := endpointKeysOnly[endpointKeyString(key)]; !exists {
			result.ManagementPreserved = false
			result.ReasonCodes = append(result.ReasonCodes, "management_endpoint_not_preserved")
		}
		if !hasScopedTrustedSource(endpoint, trusted) {
			result.ManagementPreserved = false
			result.ReasonCodes = append(result.ReasonCodes, "management_trusted_source_missing")
		}
		if endpoint.ServiceKind == hostresources.ManagementSSH {
			sshPreserved = true
		}
	}
	if requireSSH && !sshPreserved {
		result.ManagementPreserved = false
		result.ReasonCodes = append(result.ReasonCodes, "ssh_management_endpoint_missing")
	}

	for _, resource := range resources {
		if resource.ID == "" || !exactHexRevision(resource.Capabilities.ConfigRevision) {
			result.ExactRevisions = false
			result.ReasonCodes = append(result.ReasonCodes, "configuration_revision_incomplete")
		}
		keys, complete := hostresources.DeterministicConfiguredEndpointKeys(resource)
		if !complete {
			result.EndpointInventoryComplete = false
			result.ReasonCodes = append(result.ReasonCodes, "endpoint_inventory_incomplete")
			continue
		}
		for _, key := range keys {
			lookup := resource.ID + "\x00" + endpointKeyString(key)
			if _, exists := endpointKeys[lookup]; !exists {
				result.EndpointInventoryComplete = false
				result.ReasonCodes = append(result.ReasonCodes, "endpoint_inventory_incomplete")
			}
			if isManagementResource(resource.Kind) {
				if _, exists := managementKeys[lookup]; !exists {
					result.ManagementPreserved = false
					result.ReasonCodes = append(result.ReasonCodes, "management_endpoint_inventory_incomplete")
				}
			}
		}
	}

	for _, endpoint := range management {
		if !freshRecoveryForManagement(endpoint, recovery, now) {
			result.MutationReady = false
			result.MutationReasonCodes = append(result.MutationReasonCodes, "fresh_recovery_path_missing")
		}
	}
	if !result.EndpointInventoryComplete || !result.ManagementPreserved || !result.ExactRevisions || !result.ManagedTableOnly || !result.NoForeignMutation {
		result.CandidateEligible = false
		result.MutationReady = false
	}
	result.ReasonCodes = uniqueSorted(result.ReasonCodes)
	result.MutationReasonCodes = uniqueSorted(result.MutationReasonCodes)
	result.AdvisoryCodes = baselineGraphAdvisories(graph)
	result.Revision = hostresources.Revision(baselineEligibilityBinding(result))
	return result
}

func managementEndpointUsable(endpoint hostresources.ManagementEndpointV1) bool {
	if endpoint.Schema != hostresources.ManagementEndpointSchemaV1 || !exactHexRevision(endpoint.ConfigurationRevision) || endpoint.ConfidenceBP <= 0 || endpoint.ObservedAt <= 0 || endpoint.Port == 0 || endpoint.Network == hostresources.NetworkUnknown || endpoint.Family == hostresources.AddressFamilyUnknown {
		return false
	}
	for _, reason := range endpoint.ReasonCodes {
		lower := strings.ToLower(strings.TrimSpace(reason))
		if strings.Contains(lower, "unknown") || strings.Contains(lower, "stale") || strings.Contains(lower, "truncated") || strings.Contains(lower, "ambiguous") || strings.Contains(lower, "unavailable") || strings.Contains(lower, "invalid") || strings.Contains(lower, "not_verified") {
			return false
		}
	}
	return true
}

func hasScopedTrustedSource(endpoint hostresources.ManagementEndpointV1, trusted []string) bool {
	for _, value := range trusted {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err == nil && prefix.Masked().String() == strings.TrimSpace(value) &&
			((endpoint.Family == hostresources.AddressFamilyIPv4) == prefix.Addr().Is4()) {
			return true
		}
	}
	return false
}

func EvaluateListenerTopologyMutationEligibility(graph protectionresources.SocketOwnershipGraph) ListenerTopologyMutationEligibility {
	reasons := append([]string(nil), graph.ReasonCodes...)
	eligible := !graph.ApplyBlocked && exactHexRevision(graph.Revision) && exactHexRevision(graph.OwnerObservationRevision) && len(graph.Nodes) > 0
	for _, node := range graph.Nodes {
		if node.ApplyBlocked || len(node.DesiredClaims) == 0 || len(node.ObservedClaims) == 0 {
			eligible = false
			reasons = append(reasons, node.ReasonCodes...)
		}
		for _, claim := range node.ObservedClaims {
			if claim.Ambiguous || claim.OwnerObservationRevision == "" || claim.SocketInode == "" || claim.SocketCookie == 0 {
				eligible = false
				reasons = append(reasons, "exact_listener_owner_required")
			}
		}
	}
	if !eligible && len(reasons) == 0 {
		reasons = append(reasons, "exact_listener_owner_required")
	}
	result := ListenerTopologyMutationEligibility{
		Kind: ListenerTopologyMutationEligibilityKind, Eligible: eligible,
		GraphRevision: graph.Revision, OwnerObservationRevision: graph.OwnerObservationRevision,
		ReasonCodes: uniqueSorted(reasons),
	}
	result.Revision = hostresources.Revision(struct {
		Kind         EligibilityKind
		Eligible     bool
		Graph, Owner string
		Reasons      []string
	}{result.Kind, result.Eligible, result.GraphRevision, result.OwnerObservationRevision, result.ReasonCodes})
	return result
}

func freshRecoveryForManagement(endpoint hostresources.ManagementEndpointV1, paths []hostresources.RecoveryPathV1, now time.Time) bool {
	for _, path := range paths {
		if path.EndpointID != endpoint.ID || !strings.EqualFold(path.Kind, string(endpoint.ServiceKind)) ||
			path.ConfigurationRevision != endpoint.ConfigurationRevision || !hostresources.RecoveryPathFresh(path, now) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(path.VerificationMethod), "provider_console") {
			return true
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(path.SourcePrefix))
		if err == nil && prefix.Masked().String() == strings.TrimSpace(path.SourcePrefix) &&
			((endpoint.Family == hostresources.AddressFamilyIPv4) == prefix.Addr().Is4()) {
			return true
		}
	}
	return false
}

func baselineGraphAdvisories(graph protectionresources.SocketOwnershipGraph) []string {
	values := append([]string(nil), graph.ReasonCodes...)
	for _, node := range graph.Nodes {
		values = append(values, node.ReasonCodes...)
	}
	for _, collision := range graph.Collisions {
		values = append(values, collision.Code)
	}
	return uniqueSorted(values)
}

func isManagementResource(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "panel_web", "subscription":
		return true
	default:
		return false
	}
}

func sortedBaselineKeys(values []hostresources.PublicEndpointKey) []hostresources.PublicEndpointKey {
	result := append([]hostresources.PublicEndpointKey(nil), values...)
	sort.Slice(result, func(i, j int) bool { return endpointKeyString(result[i]) < endpointKeyString(result[j]) })
	return result
}
