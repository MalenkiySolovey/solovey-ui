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
	"github.com/gin-gonic/gin"
)

const firewallBaselineSnapshotBindingSchemaV1 = protectionfirewall.FirewallBaselineSnapshotBindingSchemaV1

type firewallBaselineSnapshotBinding = protectionfirewall.BaselineSnapshotBindingV1

func (h Handler) firewallBaselinePlan(c *gin.Context) {
	if !h.readAllowed(c) {
		return
	}
	state, err := h.currentFirewallBaselineState(c.Request.Context(), queryBool(c, "refresh"), nil)
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	preview := protectionfirewall.Preview(state.Plan, protectionfirewall.PreviewOptions{IncludeGeneratedNFT: queryBool(c, "include_generated_nft"), OperatingSystem: runtime.GOOS})
	capability := h.baselineService().CapabilityAssessment(c.Request.Context(), state.Plan)
	recommendations := make([]gin.H, 0, len(state.Graph.Nodes))
	for _, node := range state.Graph.Nodes {
		recommendations = append(recommendations, gin.H{"resourceId": node.ResourceID, "strategy": node.SelectedStrategy, "reasonCodes": node.ReasonCodes, "alternatives": node.Alternatives, "applyBlocked": node.ApplyBlocked, "ownerRevision": node.OwnerRevision, "configurationRevision": node.ConfigurationRevision})
	}
	h.deps.JSONObj(c, gin.H{
		"recommendations":                     recommendations,
		"socketGraph":                         state.Graph,
		"socketGraphEvidence":                 state.GraphEvidence,
		"collisionAlternatives":               state.Graph.Collisions,
		"kernelPreview":                       preview,
		"kernelPlan":                          state.Plan,
		"firewallBaselineEligibility":         state.Plan.BaselineEligibility,
		"listenerTopologyMutationEligibility": state.Topology,
		"snapshotBinding":                     state.Binding,
		"capabilityAssessment":                capability,
		"managementGuard":                     gin.H{"managementEndpoints": state.Management, "recoveryPaths": state.Recovery, "invalidRecoveryRecords": state.InvalidRecovery, "state": recoveryState(state.Recovery, state.Management, time.Now().UTC())},
		"status":                              gin.H{"desired": "COEXISTENCE_ENDPOINT_MANAGED", "selected": map[bool]string{true: "OBSERVE_ONLY", false: "PLANNED"}[!state.Plan.BaselineEligibility.CandidateEligible], "actual": "NOT_APPLIED"},
		"realNftablesLive":                    "NOT_RUN",
		"stabilityClaim":                      "normal_ci_only",
	}, nil)
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
	preview, err := h.baselineService().ResolveDecisionPreview(c.Request.Context(), input.Decision, input.ExpectedBindingRevision, now)
	if errors.Is(err, protectionfirewall.ErrPlanRevision) {
		writeError(c, http.StatusConflict, "revision_conflict", protectionfirewall.ErrPlanRevision)
		return
	}
	if errors.Is(err, protectionfirewall.ErrUnknownBaselineEndpoint) {
		writeError(c, http.StatusConflict, "unknown_endpoint", err)
		return
	}
	if err != nil {
		h.deps.JSONObj(c, nil, err)
		return
	}
	h.audit(c, "server_protection_firewall_baseline_resolve_preview", map[string]any{"decisionId": input.Decision.DecisionID, "resourceId": input.Decision.Scope.TargetResourceID, "desired": preview.Resolution.DesiredIntent, "selected": preview.Resolution.SelectedIntent, "actual": preview.Resolution.ActualStatus})
	h.deps.JSONObj(c, preview, nil)
}

func (h Handler) currentEndpointFirewallPlan(ctx context.Context, refresh bool, selected map[string]struct{}) (protectionfirewall.FirewallPlan, []hostresources.ProtectableResource, error) {
	state, err := h.currentFirewallBaselineState(ctx, refresh, selected)
	if err != nil {
		return protectionfirewall.FirewallPlan{}, nil, err
	}
	return state.Plan, append([]hostresources.ProtectableResource(nil), state.Plan.Resources...), nil
}

func (h Handler) baselineService() *protectionfirewall.BaselineService {
	if h.deps.Baseline != nil {
		return h.deps.Baseline
	}
	return protectionfirewall.NewBaselineService(h.deps.Repository)
}

func (h Handler) currentFirewallBaselineState(ctx context.Context, refresh bool, selected map[string]struct{}) (protectionfirewall.BaselineState, error) {
	return h.baselineService().Snapshot(ctx, refresh, selected)
}

func (h Handler) managementContracts(ctx context.Context, resources []hostresources.ProtectableResource, surfaces hostfacts.Snapshot, now time.Time) ([]hostresources.ManagementEndpointV1, []hostresources.RecoveryPathV1, int, error) {
	return h.baselineService().ManagementContracts(ctx, resources, surfaces, now)
}

func (h Handler) firewallBaselinePolicyInputs(ctx context.Context, now time.Time) ([]string, []protectionfirewall.EndpointActionInput, error) {
	return h.baselineService().PolicyInputs(ctx, now)
}
