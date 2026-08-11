package graylist

import (
	"errors"
	"slices"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

var ErrSignalOutOfOrder = errors.New("signal_out_of_order")

type PolicyV2 struct {
	Revision         string
	EnterScore       int
	ExitScore        int
	RateScore        int
	RateConfidenceBP int
	MaxScore         int
	DecayInterval    time.Duration
	TTL              time.Duration
}

func DefaultPolicyV2() PolicyV2 {
	return PolicyV2{
		Revision: "graylist-policy-v2", EnterScore: 20, ExitScore: 10,
		RateScore: 40, RateConfidenceBP: 5000, MaxScore: 100,
		DecayInterval: 10 * time.Minute, TTL: time.Hour,
	}
}

func (policy PolicyV2) Validate() error {
	if !domain.ValidContractID(policy.Revision, 128) || policy.ExitScore < 0 ||
		policy.EnterScore <= policy.ExitScore || policy.RateScore <= policy.EnterScore ||
		policy.MaxScore < policy.RateScore || policy.MaxScore > 100 ||
		policy.RateConfidenceBP < 0 || policy.RateConfidenceBP > 10000 ||
		policy.DecayInterval <= 0 || policy.TTL < time.Minute || policy.TTL > 24*time.Hour {
		return errors.New("graylist policy is invalid")
	}
	return nil
}

type EvaluateInput struct {
	Existing           *domain.GraylistStateV2
	Accepted           *AcceptedSignal
	Policy             PolicyV2
	StrategyRevision   string
	CapabilityRevision string
	Allowlisted        bool
	Management         bool
	RecoveryProtected  bool
	TrustedSource      bool
	ManualClear        bool
	Now                time.Time
}

type EvaluateResult struct {
	State   domain.GraylistStateV2
	Changed bool
}

// Evaluate performs a deterministic state transition. A timer evaluation only
// writes when a lifecycle transition or exact expiry occurs.
func Evaluate(input EvaluateInput) (EvaluateResult, error) {
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	policy := input.Policy
	if policy.Revision == "" {
		policy = DefaultPolicyV2()
	}
	if err := policy.Validate(); err != nil {
		return EvaluateResult{}, err
	}
	if !domain.ValidContractID(input.StrategyRevision, 128) || !domain.ValidContractID(input.CapabilityRevision, 128) {
		return EvaluateResult{}, errors.New("strategy or capability revision is invalid")
	}
	if input.Existing == nil && input.Accepted == nil {
		return EvaluateResult{}, errors.New("new graylist state requires an accepted signal")
	}

	var state domain.GraylistStateV2
	if input.Existing != nil {
		state = *input.Existing
		state.SignalRefs = append([]string(nil), state.SignalRefs...)
		state.EvidenceRefs = append([]domain.GraylistEvidenceRefV2(nil), state.EvidenceRefs...)
		state.ReasonCodes = append([]string(nil), state.ReasonCodes...)
		if err := state.Validate(); err != nil {
			return EvaluateResult{}, err
		}
		if state.PolicyRevision != policy.Revision || state.StrategyRevision != input.StrategyRevision || state.CapabilityRevision != input.CapabilityRevision {
			return supersede(state, now, "revision_drift")
		}
	}
	if input.Accepted != nil {
		signal := input.Accepted.Signal
		if signal.Provenance.PolicyRevision != policy.Revision {
			return EvaluateResult{}, errors.New("signal policy attribution does not match current policy")
		}
		if state.StateID == "" {
			state = domain.GraylistStateV2{
				Schema: domain.GraylistStateSchemaV2, Revision: 1, Subject: signal.Subject,
				ResourceID: signal.Scope.TargetResourceID, EndpointID: signal.Scope.EndpointID, Transport: signal.Scope.Transport,
				PolicyRevision: policy.Revision, StrategyRevision: input.StrategyRevision, CapabilityRevision: input.CapabilityRevision,
				SourceClasses: []string{signal.Source.SourceClass},
				Band:          domain.GraylistBandObserve, Lifecycle: domain.GraylistLifecycleActive,
				EnteredAt: signal.ObservedAt.UTC(), LastSignalAt: signal.ObservedAt.UTC(),
				ExpiresAt:        minTime(signal.ObservedAt.Add(policy.TTL), signal.ObservedAt.Add(24*time.Hour)),
				SelectedResponse: domain.IntentObserve, DesiredAction: domain.IntentObserve, ActualActionState: "NOT_APPLIED",
				CreatedAt: signal.ObservedAt.UTC(), UpdatedAt: signal.ObservedAt.UTC(),
			}
			state.FinalizeID()
		} else if state.PolicyRevision != policy.Revision || state.StrategyRevision != input.StrategyRevision || state.CapabilityRevision != input.CapabilityRevision {
			return supersede(state, now, "revision_drift")
		}
		if signal.Subject != state.Subject || signal.Scope.TargetResourceID != state.ResourceID ||
			signal.Scope.EndpointID != state.EndpointID || signal.Scope.Transport != state.Transport {
			return EvaluateResult{}, errors.New("accepted signal crosses graylist identity")
		}
		if state.Lifecycle == domain.GraylistLifecycleSuperseded {
			return EvaluateResult{}, errors.New("superseded graylist state cannot be reactivated")
		}
		if state.Lifecycle == domain.GraylistLifecycleExpired {
			state.Score = 0
			state.ConfidenceBP = 0
			state.SignalRefs = nil
			state.EvidenceRefs = nil
			state.SourceClasses = []string{signal.Source.SourceClass}
			state.Band = domain.GraylistBandObserve
			state.Lifecycle = domain.GraylistLifecycleActive
			state.EnteredAt = signal.ObservedAt.UTC()
			state.ExpiresAt = minTime(signal.ObservedAt.Add(policy.TTL), signal.ObservedAt.Add(24*time.Hour))
			state.ReasonCodes = domain.CanonicalBoundedReasons("fresh_signal_reentry")
		}
		if slices.Contains(state.SignalRefs, signal.SignalID) {
			return EvaluateResult{State: state, Changed: false}, nil
		}
		if signal.ObservedAt.Before(state.LastSignalAt) {
			return EvaluateResult{}, ErrSignalOutOfOrder
		}
		if len(state.SignalRefs) >= domain.MaxGraylistSignalRefs {
			return EvaluateResult{}, errors.New("graylist signal reference capacity exceeded")
		}
		state.Score = decayedScore(state.Score, state.LastSignalAt, signal.ObservedAt, policy)
		state.Score = min(policy.MaxScore, state.Score+input.Accepted.Delta)
		state.ConfidenceBP = max(state.ConfidenceBP, signal.ConfidenceBP)
		state.SignalRefs = domain.CanonicalSignalRefs(append(state.SignalRefs, signal.SignalID))
		state.EvidenceRefs = domain.CanonicalGraylistEvidenceRefs(append(state.EvidenceRefs, domain.GraylistEvidenceRefV2{
			SignalID: signal.SignalID, Class: evidenceClass(*input.Accepted), ExpiresAt: signal.ExpiresAt.UTC(),
		}))
		state.SourceClasses = canonicalSourceClasses(append(state.SourceClasses, signal.Source.SourceClass))
		state.LastSignalAt = signal.ObservedAt.UTC()
		state.ExpiresAt = minTime(signal.ObservedAt.Add(policy.TTL), state.EnteredAt.Add(24*time.Hour))
		state.UpdatedAt = maxTime(now, state.LastSignalAt)
	}

	if input.Allowlisted || input.Management || input.RecoveryProtected || input.TrustedSource || input.ManualClear {
		reason := "policy_precedence"
		switch {
		case input.Allowlisted:
			reason = "allowlist_precedence"
		case input.Management:
			reason = "management_precedence"
		case input.RecoveryProtected:
			reason = "recovery_precedence"
		case input.TrustedSource:
			reason = "trusted_source_precedence"
		case input.ManualClear:
			reason = "manual_clear"
		}
		return supersede(state, now, reason)
	}
	if !now.Before(state.ExpiresAt) {
		if state.Lifecycle == domain.GraylistLifecycleExpired {
			return EvaluateResult{State: state, Changed: false}, nil
		}
		state.Band = domain.GraylistBandCooldown
		state.Lifecycle = domain.GraylistLifecycleExpired
		state.SelectedResponse = domain.IntentObserve
		state.DesiredAction = domain.IntentObserve
		state.UpdatedAt = now
		state.Revision++
		state.ReasonCodes = domain.CanonicalBoundedReasons(append(state.ReasonCodes, "exact_expiry")...)
		return validated(state, true)
	}

	effective := decayedScore(state.Score, state.LastSignalAt, now, policy)
	hasStrong := hasActiveStrongEvidence(state.EvidenceRefs, now)
	nextBand := state.Band
	nextLifecycle := state.Lifecycle
	nextDesired := domain.IntentObserve
	switch {
	case !hasStrong:
		nextBand, nextLifecycle, nextDesired = domain.GraylistBandObserve, domain.GraylistLifecycleActive, domain.IntentObserve
	case effective >= policy.RateScore && state.ConfidenceBP >= policy.RateConfidenceBP:
		nextBand, nextLifecycle, nextDesired = domain.GraylistBandRateCandidate, domain.GraylistLifecycleActive, domain.IntentRateLimit
	case effective >= policy.EnterScore:
		nextBand, nextLifecycle, nextDesired = domain.GraylistBandGraylist, domain.GraylistLifecycleActive, domain.IntentSoftGraylist
	case state.Band == domain.GraylistBandGraylist || state.Band == domain.GraylistBandRateCandidate || state.Band == domain.GraylistBandCooldown:
		if effective < policy.ExitScore {
			nextBand, nextLifecycle = domain.GraylistBandCooldown, domain.GraylistLifecycleCooldown
		}
	}
	changed := input.Accepted != nil || nextBand != state.Band || nextLifecycle != state.Lifecycle || nextDesired != state.DesiredAction
	state.Score = effective
	state.Band, state.Lifecycle, state.DesiredAction = nextBand, nextLifecycle, nextDesired
	state.SelectedResponse = domain.IntentObserve
	state.ActualActionState = "NOT_APPLIED"
	state.AppliedActionRefID = ""
	if changed {
		state.Revision++
		state.UpdatedAt = maxTime(now, state.LastSignalAt)
	}
	return validated(state, changed)
}

func supersede(state domain.GraylistStateV2, now time.Time, reason string) (EvaluateResult, error) {
	if state.Lifecycle == domain.GraylistLifecycleSuperseded && slices.Contains(state.ReasonCodes, reason) {
		return EvaluateResult{State: state, Changed: false}, nil
	}
	state.Lifecycle = domain.GraylistLifecycleSuperseded
	state.SelectedResponse = domain.IntentObserve
	state.DesiredAction = domain.IntentObserve
	state.ActualActionState = "NOT_APPLIED"
	state.AppliedActionRefID = ""
	state.ReasonCodes = domain.CanonicalBoundedReasons(append(state.ReasonCodes, reason)...)
	state.UpdatedAt = maxTime(now, state.LastSignalAt)
	state.Revision++
	return validated(state, true)
}

func validated(state domain.GraylistStateV2, changed bool) (EvaluateResult, error) {
	state.SignalRefs = domain.CanonicalSignalRefs(state.SignalRefs)
	state.EvidenceRefs = domain.CanonicalGraylistEvidenceRefs(state.EvidenceRefs)
	state.ReasonCodes = domain.CanonicalBoundedReasons(state.ReasonCodes...)
	if err := state.Validate(); err != nil {
		return EvaluateResult{}, err
	}
	return EvaluateResult{State: state, Changed: changed}, nil
}

func evidenceClass(accepted AcceptedSignal) domain.GraylistEvidenceClassV2 {
	if accepted.EvidenceClass != "" {
		return accepted.EvidenceClass
	}
	if accepted.Signal.Source.SourceClass == "external" {
		return domain.GraylistEvidenceExternal
	}
	if accepted.Weak {
		return domain.GraylistEvidenceWeak
	}
	return domain.GraylistEvidenceStrongTrusted
}

func hasActiveStrongEvidence(values []domain.GraylistEvidenceRefV2, now time.Time) bool {
	for _, value := range values {
		if value.Class == domain.GraylistEvidenceStrongTrusted && value.ExpiresAt.After(now) {
			return true
		}
	}
	return false
}

func decayedScore(score int, since, now time.Time, policy PolicyV2) int {
	if since.IsZero() || !now.After(since) {
		return min(max(score, 0), policy.MaxScore)
	}
	return max(0, min(score, policy.MaxScore)-int(now.Sub(since)/policy.DecayInterval))
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func maxTime(left, right time.Time) time.Time {
	if left.After(right) {
		return left
	}
	return right
}

func canonicalSourceClasses(values []string) []string {
	hasExternal, hasNative := false, false
	for _, value := range values {
		hasExternal = hasExternal || value == "external"
		hasNative = hasNative || value == "native"
	}
	result := make([]string, 0, 2)
	if hasExternal {
		result = append(result, "external")
	}
	if hasNative {
		result = append(result, "native")
	}
	return result
}
