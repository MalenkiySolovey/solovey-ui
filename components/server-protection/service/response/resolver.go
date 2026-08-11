package response

import (
	"sort"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionpolicy "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/policy"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

const SNIPrereadActionMapDecisionV2 = "DOWNGRADE_ONLY"

type StrategyCapability struct {
	Strategy      protectionresources.Strategy      `json:"strategy"`
	Intent        domain.ResponseIntent             `json:"intent"`
	Implemented   bool                              `json:"implemented"`
	RequiresApply bool                              `json:"requiresApply"`
	ReasonCodes   []string                          `json:"reasonCodes,omitempty"`
	Facts         domain.StrategyActionCapabilityV2 `json:"facts"`
}

type ResolveInput struct {
	Decision              domain.ProtectionDecisionV2
	Strategy              protectionresources.Strategy
	Capability            domain.StrategyActionCapabilityV2
	ActionScopeRevision   string
	EndpointRevision      string
	ResourceRevision      string
	ConfigurationRevision string
	BaselineApplyBlocked  bool
	EndpointKnown         bool
	Guard                 protectionpolicy.ManagementGuardResult
	Now                   time.Time
}

type Resolution struct {
	Decision        domain.ProtectionDecisionV2 `json:"decision"`
	Capability      StrategyCapability          `json:"capability"`
	PlannedResponse *domain.PlannedResponseV2   `json:"plannedResponse,omitempty"`
	DesiredIntent   domain.ResponseIntent       `json:"desiredIntent"`
	SelectedIntent  domain.ResponseIntent       `json:"selectedIntent"`
	ActualStatus    string                      `json:"actualStatus"`
	ReasonCodes     []string                    `json:"reasonCodes"`
}

// Resolve keeps intent, explicit capability, planned response, and executor
// actual state separate. It never creates an AppliedActionV1.
func Resolve(input ResolveInput) Resolution {
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	decision := input.Decision
	desired := decision.RequestedIntent
	selected, reasons := supportedFallback(input.Strategy, input.Capability, desired, now)
	decisionValid := decision.Validate(now) == nil
	if !decisionValid {
		selected = domain.IntentObserve
		reasons = append(reasons, "decision_contract_invalid")
	}
	if !decision.ExpiresAt.After(now) {
		selected = domain.IntentObserve
		reasons = append(reasons, "decision_expired")
	}
	if decision.State != domain.DecisionCandidate {
		selected = domain.IntentObserve
		reasons = append(reasons, "decision_state_not_actionable")
	}
	if decision.AllowlistResult.Result != "deny" {
		selected = domain.IntentObserve
		reasons = append(reasons, "allowlist_precedence")
	}
	if decision.RecoveryResult.Result != "allow" {
		selected = domain.IntentObserve
		reasons = append(reasons, "recovery_precedence")
	}
	if decision.Scope.Scope != domain.ScopeEndpoint || strings.TrimSpace(decision.Scope.TargetResourceID) == "" ||
		strings.TrimSpace(decision.Scope.EndpointID) == "" {
		selected = domain.IntentObserve
		reasons = append(reasons, "scope_action_unavailable")
	}
	if input.BaselineApplyBlocked && selected != domain.IntentObserve {
		selected = domain.IntentObserve
		reasons = append(reasons, "kernel_apply_gate_disabled")
	}
	if !input.EndpointKnown {
		selected = domain.IntentObserve
		reasons = append(reasons, "endpoint_inventory_unknown")
	}
	revisionBound := domain.ValidExactRevision(input.ActionScopeRevision) && domain.ValidExactRevision(input.EndpointRevision) &&
		domain.ValidExactRevision(input.ResourceRevision) && domain.ValidExactRevision(input.ConfigurationRevision) &&
		decision.StrategyRevision == input.Capability.StrategyRevision &&
		decision.CapabilityRevision == input.Capability.CapabilityRevision &&
		decision.ActionScopeRevision == input.ActionScopeRevision &&
		decision.EndpointRevision == input.EndpointRevision &&
		decision.ResourceRevision == input.ResourceRevision &&
		decision.ConfigurationRevision == input.ConfigurationRevision &&
		input.Capability.ActionRevision == input.ActionScopeRevision &&
		input.Capability.ActionScopeRevision == input.ActionScopeRevision &&
		input.Capability.EndpointRevision == input.EndpointRevision &&
		input.Capability.ResourceRevision == input.ResourceRevision &&
		input.Capability.ConfigurationRevision == input.ConfigurationRevision &&
		input.Capability.ResourceID == decision.Scope.TargetResourceID &&
		input.Capability.EndpointID == decision.Scope.EndpointID
	if selected != domain.IntentObserve && !revisionBound {
		selected = domain.IntentObserve
		reasons = append(reasons, "action_revision_mismatch")
	}
	if input.Guard.State == protectionpolicy.ManagementGuardProtected || input.Guard.State == protectionpolicy.ManagementGuardUnknown || !input.Guard.ActionAllowed {
		selected = domain.IntentObserve
		reasons = append(reasons, input.Guard.ReasonCodes...)
		reasons = append(reasons, "management_precedence")
	}
	capability := capabilityFor(input.Strategy, input.Capability, selected, now)
	if selected != desired {
		reasons = append(reasons, "unsupported_action_downgraded")
	}
	reasons = normalizeReasons(append(reasons, capability.ReasonCodes...))
	reasons = normalizeReasons(append(reasons, "decision_not_applied"))
	decision.CapabilityResolution = domain.CapabilityResolutionV2{Implemented: capability.Implemented && selected != domain.IntentObserve, ResolvedIntent: selected, ReasonCodes: reasons}
	decision.ReasonCodes = normalizeReasons(append(decision.ReasonCodes, reasons...))
	decision.State = domain.DecisionResolved
	if selected != desired {
		decision.State = domain.DecisionDegraded
	}
	if !decision.ExpiresAt.After(now) {
		decision.State = domain.DecisionExpired
	}
	resolution := Resolution{Decision: decision, Capability: capability, DesiredIntent: desired, SelectedIntent: selected, ActualStatus: "NOT_APPLIED", ReasonCodes: reasons}
	if selected == domain.IntentObserve || !capability.Implemented {
		return resolution
	}
	response := domain.PlannedResponseV2{
		Schema: domain.PlannedResponseSchemaV2, DecisionID: decision.DecisionID,
		ResourceID: decision.Scope.TargetResourceID, EndpointID: decision.Scope.EndpointID,
		Subject: decision.Subject, DesiredIntent: desired, SelectedIntent: selected,
		CapabilityRevision: decision.CapabilityRevision, PolicyRevision: decision.PolicyRevision,
		StrategyRevision: decision.StrategyRevision, ActionScopeRevision: decision.ActionScopeRevision,
		EndpointRevision: decision.EndpointRevision, ResourceRevision: decision.ResourceRevision,
		ConfigurationRevision: decision.ConfigurationRevision, ActualState: "NOT_APPLIED",
		ReasonCodes: reasons, CreatedAt: decision.CreatedAt, ExpiresAt: decision.ExpiresAt,
	}
	response.FinalizeID()
	if err := response.Validate(); err != nil {
		resolution.SelectedIntent = domain.IntentObserve
		resolution.Capability = capabilityFor(input.Strategy, input.Capability, domain.IntentObserve, now)
		resolution.ReasonCodes = normalizeReasons(append(resolution.ReasonCodes, "planned_response_invalid"))
		resolution.Decision.CapabilityResolution = domain.CapabilityResolutionV2{Implemented: false, ResolvedIntent: domain.IntentObserve, ReasonCodes: resolution.ReasonCodes}
		resolution.Decision.State = domain.DecisionDegraded
		return resolution
	}
	resolution.PlannedResponse = &response
	return resolution
}

func capabilityFor(strategy protectionresources.Strategy, facts domain.StrategyActionCapabilityV2, intent domain.ResponseIntent, now time.Time) StrategyCapability {
	result := StrategyCapability{Strategy: strategy, Intent: intent, RequiresApply: intent != domain.IntentObserve, Facts: facts}
	if intent == domain.IntentObserve {
		result.Implemented = true
		result.RequiresApply = false
		return result
	}
	if facts.Schema == "" {
		result.ReasonCodes = []string{"action_capability_unknown"}
		return result
	}
	if facts.Validate(now) != nil || facts.Strategy != string(strategy) {
		result.ReasonCodes = []string{"action_capability_unknown"}
		return result
	}
	if !facts.ExpiresAt.After(now) {
		result.ReasonCodes = []string{"action_capability_stale"}
		return result
	}
	switch intent {
	case domain.IntentSoftGraylist, domain.IntentRateLimit, domain.IntentTemporaryQuarantine:
		result.Implemented = facts.SameScopeRateLimit
	case domain.IntentRouteToDecoy:
		result.Implemented = facts.ForcedSameSubjectDecoyRoute
	case domain.IntentTemporaryBlock, domain.IntentManualHardBlock:
		result.Implemented = facts.HardBlock
	}
	if !result.Implemented {
		result.ReasonCodes = []string{"strategy_intent_unsupported"}
	}
	return result
}

func supportedFallback(strategy protectionresources.Strategy, facts domain.StrategyActionCapabilityV2, requested domain.ResponseIntent, now time.Time) (domain.ResponseIntent, []string) {
	reasons := []string{}
	if requested == domain.IntentRouteToDecoy && facts.Schema != "" && facts.Validate(now) == nil && facts.Strategy == string(strategy) && !facts.ForcedSameSubjectDecoyRoute {
		reasons = append(reasons, "forced_subject_decoy_unavailable")
		if facts.NaturalInvalidTrafficFallback {
			reasons = append(reasons, "native_fallback_natural_only")
		}
	}
	for _, candidate := range fallbackChain(requested) {
		if capabilityFor(strategy, facts, candidate, now).Implemented {
			return candidate, reasons
		}
	}
	capability := capabilityFor(strategy, facts, requested, now)
	return domain.IntentObserve, append(reasons, capability.ReasonCodes...)
}

func fallbackChain(requested domain.ResponseIntent) []domain.ResponseIntent {
	switch requested {
	case domain.IntentObserve:
		return []domain.ResponseIntent{domain.IntentObserve}
	case domain.IntentSoftGraylist:
		return []domain.ResponseIntent{domain.IntentSoftGraylist, domain.IntentObserve}
	case domain.IntentRateLimit:
		return []domain.ResponseIntent{domain.IntentRateLimit, domain.IntentSoftGraylist, domain.IntentObserve}
	case domain.IntentRouteToDecoy:
		return []domain.ResponseIntent{domain.IntentRouteToDecoy, domain.IntentSoftGraylist, domain.IntentObserve}
	case domain.IntentTemporaryQuarantine:
		return []domain.ResponseIntent{domain.IntentTemporaryQuarantine, domain.IntentRateLimit, domain.IntentSoftGraylist, domain.IntentObserve}
	case domain.IntentTemporaryBlock:
		return []domain.ResponseIntent{domain.IntentTemporaryBlock, domain.IntentTemporaryQuarantine, domain.IntentRateLimit, domain.IntentSoftGraylist, domain.IntentObserve}
	case domain.IntentManualHardBlock:
		return []domain.ResponseIntent{domain.IntentManualHardBlock, domain.IntentTemporaryBlock, domain.IntentTemporaryQuarantine, domain.IntentRateLimit, domain.IntentSoftGraylist, domain.IntentObserve}
	default:
		return []domain.ResponseIntent{domain.IntentObserve}
	}
}

func NativeFallbackCapability(now time.Time, resourceID, endpointID string, binding domain.DecisionRevisionBindingV2, sameScopeRateLimit bool) domain.StrategyActionCapabilityV2 {
	reasons := []string{"native_fallback_natural_only", "forced_subject_decoy_unavailable"}
	value := domain.StrategyActionCapabilityV2{
		Schema: domain.StrategyActionCapabilitySchemaV2, Strategy: string(protectionresources.StrategyNativeFallback),
		ActionRevision: binding.ActionScopeRevision, StrategyRevision: binding.StrategyRevision, CapabilityRevision: binding.CapabilityRevision,
		ResourceID: resourceID, EndpointID: endpointID, ActionScopeRevision: binding.ActionScopeRevision,
		EndpointRevision: binding.EndpointRevision, ResourceRevision: binding.ResourceRevision,
		ConfigurationRevision:         binding.ConfigurationRevision,
		NaturalInvalidTrafficFallback: true, ForcedSameSubjectDecoyRoute: false,
		SameScopeRateLimit: sameScopeRateLimit, HardBlock: false,
		Provenance: "native-fallback-capability", ObservedAt: now.UTC(), ExpiresAt: now.UTC().Add(time.Hour),
		ReasonCodes: normalizeReasons(reasons),
	}
	return value
}

// SNIPrereadFrontingCapability is deliberately DOWNGRADE_ONLY. The current
// public-socket contract proves listener ownership, but does not prove direct
// exact subject observation or a trusted forwarded identity/action-map expiry
// path. Natural selector routing is therefore never projected as a forced
// same-subject route and cannot create AppliedActionV1 evidence.
func SNIPrereadFrontingCapability(now time.Time, resourceID, endpointID string, binding domain.DecisionRevisionBindingV2, sameScopeRateLimit bool) domain.StrategyActionCapabilityV2 {
	value := domain.StrategyActionCapabilityV2{
		Schema: domain.StrategyActionCapabilitySchemaV2, Strategy: string(protectionresources.StrategySNIPreReadFronting),
		ActionRevision: binding.ActionScopeRevision, StrategyRevision: binding.StrategyRevision, CapabilityRevision: binding.CapabilityRevision,
		ResourceID: resourceID, EndpointID: endpointID, ActionScopeRevision: binding.ActionScopeRevision,
		EndpointRevision: binding.EndpointRevision, ResourceRevision: binding.ResourceRevision,
		ConfigurationRevision:         binding.ConfigurationRevision,
		NaturalInvalidTrafficFallback: false, ForcedSameSubjectDecoyRoute: false,
		SameScopeRateLimit: sameScopeRateLimit, HardBlock: false,
		Provenance: "sni-preread-downgrade-only", ObservedAt: now.UTC(), ExpiresAt: now.UTC().Add(time.Hour),
		ReasonCodes: normalizeReasons([]string{"forced_subject_decoy_unavailable", "sni_action_map_not_proven"}),
	}
	return value
}

func normalizeReasons(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || !domain.ValidContractID(value, 64) {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	if len(result) > 32 {
		result = result[:32]
	}
	return result
}
