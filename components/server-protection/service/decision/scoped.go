package decision

import (
	"errors"
	"sort"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

type EscalationPolicyV1 struct {
	Revision            string        `json:"revision"`
	GraylistScore       int           `json:"graylistScore"`
	RateLimitScore      int           `json:"rateLimitScore"`
	QuarantineScore     int           `json:"quarantineScore"`
	TemporaryBlockScore int           `json:"temporaryBlockScore"`
	RateConfidenceBP    int           `json:"rateConfidenceBp"`
	StrongConfidenceBP  int           `json:"strongConfidenceBp"`
	DecisionTTL         time.Duration `json:"-"`
}

func DefaultEscalationPolicyV1() EscalationPolicyV1 {
	return EscalationPolicyV1{Revision: "endpoint-baseline-default-v1", GraylistScore: 20, RateLimitScore: 40, QuarantineScore: 70, TemporaryBlockScore: 90, RateConfidenceBP: 5000, StrongConfidenceBP: 8000, DecisionTTL: time.Hour}
}

type ScopedDecisionInput struct {
	Subject         domain.SignalSubjectV2
	Scope           domain.SignalScopeV2
	Signals         []domain.ProtectionSignalV2
	Score           domain.ScoreSnapshotV2
	AllowlistResult domain.PolicyCheckV2
	RecoveryResult  domain.PolicyCheckV2
	RevisionBinding domain.DecisionRevisionBindingV2
	Policy          EscalationPolicyV1
	Now             time.Time
}

// ResolveScopedDecision creates a policy decision only. Strategy capability
// and applied state remain the responsibility of the response resolver and
// executor respectively.
func ResolveScopedDecision(input ScopedDecisionInput) (domain.ProtectionDecisionV2, error) {
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	policy := input.Policy
	if policy.Revision == "" {
		policy = DefaultEscalationPolicyV1()
	}
	if err := validateEscalationPolicy(policy); err != nil {
		return domain.ProtectionDecisionV2{}, err
	}
	if input.Scope.Scope != domain.ScopeEndpoint || input.Scope.TargetResourceID == "" ||
		input.Scope.EndpointID == "" || input.Scope.Transport == "" {
		return domain.ProtectionDecisionV2{}, errors.New("firewall baseline automatic decisions require one exact endpoint scope")
	}
	refs := make([]string, 0, len(input.Signals))
	sourceSet := make(map[string]struct{})
	confidence := 10000
	for _, signal := range input.Signals {
		if err := signal.Validate(now); err != nil || !signal.ExpiresAt.After(now) || !signal.KnownKind {
			return domain.ProtectionDecisionV2{}, errors.New("decision input contains an invalid or stale signal")
		}
		if signal.Subject != input.Subject || signal.Scope != input.Scope {
			return domain.ProtectionDecisionV2{}, errors.New("decision input crosses subject or endpoint scope")
		}
		refs = append(refs, signal.SignalID)
		sourceSet[signal.Source.SourceClass] = struct{}{}
		if signal.ConfidenceBP < confidence {
			confidence = signal.ConfidenceBP
		}
	}
	if len(refs) == 0 {
		return domain.ProtectionDecisionV2{}, errors.New("decision requires at least one scoped signal")
	}
	sort.Strings(refs)
	sources := make([]string, 0, len(sourceSet))
	for source := range sourceSet {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	requested, reasons := policyIntent(input.Score.Score, confidence, policy)
	if input.AllowlistResult.Result != "deny" {
		requested = domain.IntentObserve
		reasons = append(reasons, "allowlist_not_cleared")
	}
	if input.RecoveryResult.Result != "allow" {
		requested = domain.IntentObserve
		reasons = append(reasons, "recovery_precedence")
	}
	reasons = domain.NormalizeActionReasons(reasons)
	created := input.Score.CapturedAt.UTC()
	if created.IsZero() {
		created = now
	}
	expires := created.Add(policy.DecisionTTL)
	if expires.After(created.Add(24 * time.Hour)) {
		expires = created.Add(24 * time.Hour)
	}
	if !expires.After(now) {
		return domain.ProtectionDecisionV2{}, errors.New("decision score snapshot is already expired")
	}
	decision := domain.ProtectionDecisionV2{Schema: domain.ProtectionDecisionSchemaV2, PolicyRevision: policy.Revision,
		StrategyRevision: input.RevisionBinding.StrategyRevision, CapabilityRevision: input.RevisionBinding.CapabilityRevision,
		ActionScopeRevision: input.RevisionBinding.ActionScopeRevision, EndpointRevision: input.RevisionBinding.EndpointRevision,
		ResourceRevision: input.RevisionBinding.ResourceRevision, ConfigurationRevision: input.RevisionBinding.ConfigurationRevision,
		Subject: input.Subject, Scope: input.Scope, TargetResourceIDs: []string{input.Scope.TargetResourceID}, SignalRefs: refs, SourceClasses: sources, ScoreSnapshot: input.Score, ConfidenceBP: confidence, ReasonCodes: reasons, RequestedIntent: requested, CreatedAt: created, ExpiresAt: expires, AllowlistResult: input.AllowlistResult, RecoveryResult: input.RecoveryResult, CapabilityResolution: domain.CapabilityResolutionV2{Implemented: false, ResolvedIntent: domain.IntentObserve, ReasonCodes: []string{domain.ReasonCapabilityUnavailable}}, State: domain.DecisionCandidate}
	decision.FinalizeID()
	if err := decision.Validate(now); err != nil {
		return domain.ProtectionDecisionV2{}, err
	}
	return decision, nil
}

// ResolveGraylistState projects current policy state into a decision. It does
// not infer strategy capability and cannot create an applied action.
func ResolveGraylistState(state domain.GraylistStateV2, allowlist, recovery domain.PolicyCheckV2, binding domain.DecisionRevisionBindingV2, now time.Time) (domain.ProtectionDecisionV2, error) {
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := state.Validate(); err != nil {
		return domain.ProtectionDecisionV2{}, err
	}
	if state.Lifecycle == domain.GraylistLifecycleExpired || state.Lifecycle == domain.GraylistLifecycleSuperseded || state.Lifecycle == domain.GraylistLifecycleLegacyStale {
		return domain.ProtectionDecisionV2{}, errors.New("terminal or legacy graylist state is not decision-actionable")
	}
	requested := state.DesiredAction
	reasons := append([]string(nil), state.ReasonCodes...)
	if allowlist.Result != "deny" {
		requested = domain.IntentObserve
		reasons = append(reasons, "allowlist_precedence")
	}
	if recovery.Result != "allow" {
		requested = domain.IntentObserve
		reasons = append(reasons, "recovery_precedence")
	}
	created := state.UpdatedAt.UTC()
	expires := state.ExpiresAt.UTC()
	if !expires.After(created) {
		return domain.ProtectionDecisionV2{}, errors.New("graylist state has no current decision lifetime")
	}
	decision := domain.ProtectionDecisionV2{
		Schema: domain.ProtectionDecisionSchemaV2, PolicyRevision: state.PolicyRevision,
		StrategyRevision: binding.StrategyRevision, CapabilityRevision: binding.CapabilityRevision,
		ActionScopeRevision: binding.ActionScopeRevision, EndpointRevision: binding.EndpointRevision,
		ResourceRevision: binding.ResourceRevision, ConfigurationRevision: binding.ConfigurationRevision,
		Subject: state.Subject, Scope: domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: state.ResourceID, EndpointID: state.EndpointID, Transport: state.Transport},
		TargetResourceIDs: []string{state.ResourceID}, SignalRefs: append([]string(nil), state.SignalRefs...),
		SourceClasses: append([]string(nil), state.SourceClasses...),
		ScoreSnapshot: domain.ScoreSnapshotV2{Score: state.Score, TargetGroup: state.ResourceID, CapturedAt: created},
		ConfidenceBP:  state.ConfidenceBP, ReasonCodes: domain.NormalizeActionReasons(reasons),
		RequestedIntent: requested, CreatedAt: created, ExpiresAt: expires,
		AllowlistResult: allowlist, RecoveryResult: recovery,
		CapabilityResolution: domain.CapabilityResolutionV2{Implemented: false, ResolvedIntent: domain.IntentObserve, ReasonCodes: []string{domain.ReasonCapabilityUnavailable}},
		State:                domain.DecisionCandidate,
	}
	decision.FinalizeID()
	if err := decision.Validate(now); err != nil {
		return domain.ProtectionDecisionV2{}, err
	}
	return decision, nil
}

func validateEscalationPolicy(value EscalationPolicyV1) error {
	if !domain.ValidContractID(value.Revision, 128) || value.GraylistScore < 0 || value.RateLimitScore <= value.GraylistScore || value.QuarantineScore <= value.RateLimitScore || value.TemporaryBlockScore <= value.QuarantineScore || value.RateConfidenceBP < 0 || value.RateConfidenceBP > value.StrongConfidenceBP || value.StrongConfidenceBP > 10000 || value.DecisionTTL <= 0 || value.DecisionTTL > 24*time.Hour {
		return errors.New("escalation policy is invalid")
	}
	return nil
}

func policyIntent(score, confidence int, policy EscalationPolicyV1) (domain.ResponseIntent, []string) {
	switch {
	case score >= policy.TemporaryBlockScore && confidence >= policy.StrongConfidenceBP:
		return domain.IntentTemporaryBlock, []string{"score_temporary_block"}
	case score >= policy.QuarantineScore && confidence >= policy.StrongConfidenceBP:
		return domain.IntentTemporaryQuarantine, []string{"score_temporary_quarantine"}
	case score >= policy.RateLimitScore && confidence >= policy.RateConfidenceBP:
		return domain.IntentRateLimit, []string{"score_rate_limit"}
	case score >= policy.GraylistScore:
		return domain.IntentSoftGraylist, []string{"score_soft_graylist"}
	default:
		return domain.IntentObserve, []string{"score_observe"}
	}
}
