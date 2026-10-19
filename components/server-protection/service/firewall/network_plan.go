package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	helper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	repository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	platform "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	networkplan "github.com/MalenkiySolovey/solovey-ui/network/planner"
)

// NetworkInputs is an operation-local read of canonical owner state. The
// argument selects semantic resources; it is never returned as current facts
// by the production implementation. Deterministic fixtures inject their owner.
type NetworkInputs func(context.Context, FirewallPlan) (FirewallPlan, error)

func (s *BaselineService) NetworkInputs(ctx context.Context, _ FirewallPlan) (FirewallPlan, error) {
	state, err := s.Snapshot(ctx, true, nil)
	return state.Plan, err
}

func (w Workflow) networkFacts(ctx context.Context, desired FirewallPlan, capabilities *helper.CapabilitiesResult, allocations []networkplan.Allocation) (networkplan.Facts, error) {
	if w.CurrentNetworkInputs == nil {
		return networkplan.Facts{}, &networkplan.Conflict{Reason: networkplan.ObservationUnknown, ResourceRef: networkplan.ProtectionResourceRef}
	}
	current, err := w.CurrentNetworkInputs(ctx, desired)
	if err != nil {
		return networkplan.Facts{}, errors.Join(&networkplan.Conflict{Reason: networkplan.ObservationUnknown, ResourceRef: networkplan.ProtectionResourceRef}, err)
	}
	intent := protectionIntent(current)
	facts := networkplan.Facts{HostRef: hostref.Local(), InventoryComplete: true,
		Revision: intent.Binding.InputRevision, Resources: intent.Binding.Resources, Allocations: allocations,
		Capability: platform.PlatformFact{Availability: platform.Unavailable, Backend: "nft", Reason: "nft_capability_unavailable"}}
	if firewallCapabilitiesAvailable(capabilities, desired) {
		facts.CapabilityRevision = capabilities.Revision
		facts.Capability = platform.PlatformFact{Availability: platform.Available, Backend: "nft"}
	}
	// The existing policy owner establishes continuity. Planner consumes the
	// result for the exact semantic endpoint, without inventing management policy.
	continuity := "UNKNOWN"
	if Preflight(current) == nil && preflightTemporalPlan(current, w.now()) == nil {
		continuity = "VERIFIED"
	}
	for _, binding := range facts.Resources {
		if binding.Management {
			facts.Continuity = append(facts.Continuity, networkplan.Continuity{ResourceRef: binding.ResourceRef, EndpointRevision: binding.EndpointRevision, Status: continuity})
		}
	}
	return facts, nil
}

func expectedNetworkAllocations(snapshot repository.FirewallAuthoritySnapshot) []networkplan.Allocation {
	if !snapshot.HasComposition {
		return nil
	}
	return []networkplan.Allocation{{Ownership: networkplan.Ownership{HostRef: hostref.Local(), Owner: "server-protection", ResourceRef: networkplan.ProtectionResourceRef,
		PlanRevision: snapshot.Composition.ManagedPlanRevision, Kind: "filter_table", Purpose: "protection-enforcement", Allocation: networkplan.ProtectionAllocation},
		Proven: snapshot.Composition.State == "ACTIVE" && snapshot.HasObservation && snapshot.Observation.State == FirewallLiveMatching && snapshot.Observation.CommittedCompositionRevision == snapshot.Composition.Revision}}
}

func (w Workflow) compileNetwork(ctx context.Context, desired FirewallPlan, capabilities *helper.CapabilitiesResult, snapshot repository.FirewallAuthoritySnapshot) (networkplan.ProtectionPlan, networkplan.Facts, error) {
	facts, err := w.networkFacts(ctx, desired, capabilities, expectedNetworkAllocations(snapshot))
	if err != nil {
		return networkplan.ProtectionPlan{}, facts, err
	}
	plan, err := networkplan.CompileProtection(protectionIntent(desired), facts)
	if err == nil && desired.Mode == ModeCoexistenceEndpointManaged && desired.InputRevision != facts.Revision {
		err = &networkplan.Conflict{Reason: networkplan.StalePlan, ResourceRef: networkplan.ProtectionResourceRef}
	}
	return plan, facts, err
}

func (w Workflow) runtimeNetworkFacts(plan FirewallPlan, current RuntimeManagement, capabilities *helper.CapabilitiesResult, boot string) (networkplan.Facts, error) {
	if !validFirewallSHA(boot) || validateRuntimeManagement(plan, current, w.now()) != nil {
		return networkplan.Facts{}, &networkplan.Conflict{Reason: networkplan.ManagementUnknown, ResourceRef: networkplan.ProtectionResourceRef}
	}
	// The existing owner has just proved complete canonical resource equality
	// and current management coverage. Only after that proof may its sealed
	// semantic bindings be used for runtime continuation, without reviving a
	// persisted browser/session mutation grant.
	intent := protectionIntent(plan)
	facts := networkplan.Facts{HostRef: hostref.Local(), InventoryComplete: true, Resources: intent.Binding.Resources,
		Revision: hostresources.Revision(struct {
			Boot, Resources, Management string
			Trusted                     []string
		}{boot,
			hostresources.Revision(CanonicalPlanResources(current.Resources)), BaselineConfigurationRevision(current.Management), uniqueSorted(current.Trusted)}),
		Capability: platform.PlatformFact{Availability: platform.Unavailable, Backend: "nft"}}
	if firewallCapabilitiesAvailable(capabilities, plan) {
		facts.CapabilityRevision, facts.Capability = capabilities.Revision, platform.PlatformFact{Availability: platform.Available, Backend: "nft"}
	}
	for _, binding := range facts.Resources {
		if binding.Management {
			facts.Continuity = append(facts.Continuity, networkplan.Continuity{ResourceRef: binding.ResourceRef, EndpointRevision: binding.EndpointRevision, Status: "VERIFIED"})
		}
	}
	return facts, nil
}

func networkManifest(plan networkplan.ProtectionPlan, candidateSHA string) []byte {
	value := struct {
		Schema          string                  `json:"schema"`
		Intent          networkplan.Intent      `json:"intent"`
		Revision        string                  `json:"revision"`
		CandidateSHA256 string                  `json:"candidateSha256"`
		Resources       []networkplan.Ownership `json:"resources"`
	}{networkplan.Schema, plan.Intent(), plan.Revision(), candidateSHA, plan.Resources()}
	data, _ := json.Marshal(value)
	return append(data, '\n')
}

func networkEvidence(planRevision string, proof componenthealth.ProofKind, now time.Time) componenthealth.Result {
	result := componenthealth.Result{ResourceID: networkplan.ProtectionResourceRef, Status: componenthealth.StatusOK, Check: "network-planner", FactCode: "owned_network_state_verified"}
	if proof == componenthealth.ConfigValid {
		result.FactCode = "network_candidate_checked"
	}
	result = componenthealth.WithEvidence(result, "server-protection", proof, "nft-adapter", planRevision, now, 30*time.Second)
	result.Evidence.DesiredRevision = planRevision
	return result
}
