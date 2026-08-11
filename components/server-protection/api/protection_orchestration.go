//go:build !minimal

package api

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionpolicy "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/policy"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	protectionresponse "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/response"
	"github.com/gin-gonic/gin"
)

const firewallBaselineSnapshotBindingSchemaV1 = protectionfirewall.FirewallBaselineSnapshotBindingSchemaV1

type firewallBaselineSnapshotBinding = protectionfirewall.BaselineSnapshotBindingV1

type firewallBaselineState struct {
	graph           protectionresources.SocketOwnershipGraph
	graphEvidence   protectionresources.SocketOwnershipGraphEvidenceV1
	topology        protectionfirewall.ListenerTopologyMutationEligibility
	plan            protectionfirewall.FirewallPlan
	management      []hostresources.ManagementEndpointV1
	recovery        []hostresources.RecoveryPathV1
	trusted         []string
	invalidRecovery int
	binding         firewallBaselineSnapshotBinding
}

func (h Handler) firewallBaselinePlan(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	state, err := h.currentFirewallBaselineState(c.Request.Context(), queryBool(c, "refresh"), nil)
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	preview := protectionfirewall.Preview(state.plan, protectionfirewall.PreviewOptions{IncludeGeneratedNFT: queryBool(c, "include_generated_nft"), OperatingSystem: runtime.GOOS})
	capability := h.firewallBaselineCapabilityAssessment(c.Request.Context(), state.plan)
	recommendations := make([]gin.H, 0, len(state.graph.Nodes))
	for _, node := range state.graph.Nodes {
		recommendations = append(recommendations, gin.H{"resourceId": node.ResourceID, "strategy": node.SelectedStrategy, "reasonCodes": node.ReasonCodes, "alternatives": node.Alternatives, "applyBlocked": node.ApplyBlocked, "ownerRevision": node.OwnerRevision, "configurationRevision": node.ConfigurationRevision})
	}
	h.deps.JSONObj(c, gin.H{
		"recommendations":                     recommendations,
		"socketGraph":                         state.graph,
		"socketGraphEvidence":                 state.graphEvidence,
		"collisionAlternatives":               state.graph.Collisions,
		"kernelPreview":                       preview,
		"kernelPlan":                          state.plan,
		"firewallBaselineEligibility":         state.plan.BaselineEligibility,
		"listenerTopologyMutationEligibility": state.topology,
		"snapshotBinding":                     state.binding,
		"capabilityAssessment":                capability,
		"managementGuard":                     gin.H{"managementEndpoints": state.management, "recoveryPaths": state.recovery, "invalidRecoveryRecords": state.invalidRecovery, "state": recoveryState(state.recovery, state.management, time.Now().UTC())},
		"status":                              gin.H{"desired": "COEXISTENCE_ENDPOINT_MANAGED", "selected": map[bool]string{true: "OBSERVE_ONLY", false: "PLANNED"}[!state.plan.BaselineEligibility.CandidateEligible], "actual": "NOT_APPLIED"},
		"realNftablesLive":                    "NOT_RUN",
		"stabilityClaim":                      "normal_ci_only",
	}, nil)
}

type firewallBaselineCapabilityAssessment struct {
	CapabilityRevision   string `json:"capabilityRevision,omitempty"`
	TTLRequired          bool   `json:"ttlRequired"`
	TTLSupported         bool   `json:"ttlSupported"`
	RateRequired         bool   `json:"rateRequired"`
	RateSupported        bool   `json:"rateSupported"`
	CandidateSupported   bool   `json:"candidateSupported"`
	AdvancedState        string `json:"advancedState"`
	Consequence          string `json:"acceptanceConsequence"`
	SSHRecoverySupported bool   `json:"sshRecoverySupported"`
	SSHVerifierRevision  string `json:"sshVerifierRevision,omitempty"`
}

func (h Handler) firewallBaselineCapabilityAssessment(ctx context.Context, plan protectionfirewall.FirewallPlan) firewallBaselineCapabilityAssessment {
	if h.deps.Firewall == nil {
		return assessFirewallBaselineCapabilities(plan, nil)
	}
	capabilities, err := h.deps.Firewall.Capabilities(ctx)
	if err != nil {
		return assessFirewallBaselineCapabilities(plan, nil)
	}
	return assessFirewallBaselineCapabilities(plan, capabilities)
}

func assessFirewallBaselineCapabilities(plan protectionfirewall.FirewallPlan, capabilities *protectionhelper.CapabilitiesResult) firewallBaselineCapabilityAssessment {
	result := firewallBaselineCapabilityAssessment{AdvancedState: "DEFERRED_UNPROVEN", Consequence: "BASELINE_BLOCKED"}
	for _, endpoint := range plan.Endpoints {
		for _, contribution := range endpoint.Contributions {
			result.TTLRequired = true
			if contribution.Intent == domain.IntentSoftGraylist || contribution.Intent == domain.IntentRateLimit || contribution.Intent == domain.IntentTemporaryQuarantine {
				result.RateRequired = true
			}
		}
	}
	if capabilities == nil || !capabilities.NFT.Available || !protectionhelper.CapabilityAvailable(capabilities, protectionhelper.OperationNFTValidate) {
		return result
	}
	result.CapabilityRevision = capabilities.Revision
	result.SSHRecoverySupported = capabilities.SSHRecovery.Available && protectionhelper.CapabilityAvailable(capabilities, protectionhelper.OperationSSHRecoveryObserve) && exactAPIRevision(capabilities.SSHRecovery.VerifierRevision)
	if result.SSHRecoverySupported {
		result.SSHVerifierRevision = capabilities.SSHRecovery.VerifierRevision
	}
	result.TTLSupported = capabilities.NFT.TTLSet
	result.RateSupported = capabilities.NFT.RateLimit
	result.CandidateSupported = (!result.TTLRequired || result.TTLSupported) && (!result.RateRequired || result.RateSupported)
	if result.TTLSupported && result.RateSupported {
		result.AdvancedState = "SUPPORTED_BY_READ_ONLY_CHECK"
	} else if result.CandidateSupported {
		result.Consequence = "BASELINE_ONLY_ADVANCED_SCENARIOS_NOT_SHIPPED"
	}
	if result.CandidateSupported && result.Consequence == "BASELINE_BLOCKED" {
		result.Consequence = "CURRENT_CANDIDATE_SUPPORTED"
	}
	return result
}

type resolvePreviewInput struct {
	Decision                domain.ProtectionDecisionV2 `json:"decision"`
	ExpectedBindingRevision string                      `json:"expectedBindingRevision"`
}

func (h Handler) decisionResolvePreview(c *gin.Context) {
	if !h.writeAllowed(c) {
		return
	}
	var input resolvePreviewInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	now := time.Now().UTC()
	if err := input.Decision.Validate(now); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err)
		return
	}
	state, err := h.currentFirewallBaselineState(c.Request.Context(), true, nil)
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	if !exactAPIRevision(input.ExpectedBindingRevision) || input.ExpectedBindingRevision != state.binding.Revision {
		writeError(c, http.StatusConflict, "revision_conflict", protectionfirewall.ErrPlanRevision)
		return
	}
	resource, endpoint, found := firewallBaselineTarget(state, input.Decision.Scope.TargetResourceID)
	if !found {
		writeError(c, http.StatusConflict, "unknown_endpoint", errors.New("decision target is absent from the configured endpoint inventory"))
		return
	}
	guard := protectionpolicy.EvaluateManagementGuard(protectionpolicy.ManagementGuardInput{Scope: input.Decision.Scope, Subject: input.Decision.Subject, EndpointKey: endpoint.Key, Management: state.management, RecoveryPaths: state.recovery, TrustedSources: state.trusted, MayRestrictTraffic: input.Decision.RequestedIntent != domain.IntentObserve, Now: now})
	resourceRevision := hostresources.Revision(protectionfirewall.CanonicalPlanResources([]hostresources.ProtectableResource{resource}))
	resolution := protectionresponse.Resolve(protectionresponse.ResolveInput{Decision: input.Decision, Strategy: endpoint.Strategy, ActionScopeRevision: protectionfirewall.EndpointActionScopeRevision(state.plan.Resources), EndpointRevision: endpoint.EndpointRevision, ResourceRevision: protectionfirewall.EndpointActionResourceRevision(resource), ConfigurationRevision: resource.Capabilities.ConfigRevision, BaselineApplyBlocked: state.plan.ApplyBlocked || !state.plan.BaselineEligibility.CandidateEligible, EndpointKnown: endpoint.EndpointRevision != "", Guard: guard, Now: now})
	h.audit(c, "server_protection_firewall_baseline_resolve_preview", map[string]any{"decisionId": input.Decision.DecisionID, "resourceId": input.Decision.Scope.TargetResourceID, "desired": resolution.DesiredIntent, "selected": resolution.SelectedIntent, "actual": resolution.ActualStatus})
	h.deps.JSONObj(c, gin.H{"resolution": resolution, "managementGuard": guard, "bindingRevision": state.binding.Revision, "baselineEligibilityRevision": state.plan.BaselineEligibility.Revision, "endpointRevision": endpoint.EndpointRevision, "resourceRevision": resourceRevision, "configurationRevision": resource.Capabilities.ConfigRevision, "actual": "NOT_APPLIED"}, nil)
}

func (h Handler) currentEndpointFirewallPlan(ctx context.Context, refresh bool, selected map[string]struct{}) (protectionfirewall.FirewallPlan, []hostresources.ProtectableResource, error) {
	state, err := h.currentFirewallBaselineState(ctx, refresh, selected)
	if err != nil {
		return protectionfirewall.FirewallPlan{}, nil, err
	}
	return state.plan, append([]hostresources.ProtectableResource(nil), state.plan.Resources...), nil
}

func (h Handler) baselineService() *protectionfirewall.BaselineService {
	if h.deps.Baseline != nil {
		return h.deps.Baseline
	}
	return protectionfirewall.NewBaselineService(h.deps.Repository)
}

func (h Handler) currentFirewallBaselineState(ctx context.Context, refresh bool, selected map[string]struct{}) (firewallBaselineState, error) {
	state, err := h.baselineService().Snapshot(ctx, refresh, selected)
	if err != nil {
		return firewallBaselineState{}, err
	}
	return firewallBaselineState{
		graph: state.Graph, graphEvidence: state.GraphEvidence, topology: state.Topology, plan: state.Plan,
		management: state.Management, recovery: state.Recovery, trusted: state.Trusted,
		invalidRecovery: state.InvalidRecovery, binding: state.Binding,
	}, nil
}

func (h Handler) managementContracts(ctx context.Context, resources []hostresources.ProtectableResource, surfaces hostfacts.Snapshot, now time.Time) ([]hostresources.ManagementEndpointV1, []hostresources.RecoveryPathV1, int, error) {
	return h.baselineService().ManagementContracts(ctx, resources, surfaces, now)
}

func (h Handler) firewallBaselinePolicyInputs(ctx context.Context, now time.Time) ([]string, []protectionfirewall.EndpointActionInput, error) {
	return h.baselineService().PolicyInputs(ctx, now)
}

func firewallBaselineTarget(state firewallBaselineState, resourceID string) (hostresources.ProtectableResource, protectionfirewall.EndpointPolicy, bool) {
	var resource hostresources.ProtectableResource
	for _, candidate := range state.plan.Resources {
		if candidate.ID == resourceID {
			resource = candidate
			break
		}
	}
	if resource.ID == "" {
		return hostresources.ProtectableResource{}, protectionfirewall.EndpointPolicy{}, false
	}
	matches := make([]protectionfirewall.EndpointPolicy, 0, 1)
	for _, endpoint := range state.plan.Endpoints {
		if endpoint.ResourceID == resourceID {
			matches = append(matches, endpoint)
		}
	}
	if len(matches) == 1 {
		return resource, matches[0], true
	}
	return hostresources.ProtectableResource{}, protectionfirewall.EndpointPolicy{}, false
}
