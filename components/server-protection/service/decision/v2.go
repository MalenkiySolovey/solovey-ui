package decision

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/scoring"
)

// NormalizeScoreState produces a scoped, observe-only compatibility decision.
// Legacy action enum presence never becomes an applied or implemented action.
func NormalizeScoreState(value scoring.ScoreState, legacyDecision string, now time.Time) domain.ProtectionDecisionV2 {
	resourceID := boundedResourceID(value.ResourceID)
	created := value.LastSignalAt.UTC()
	if created.IsZero() {
		created = now.UTC()
	}
	expires := created.Add(time.Hour)
	if value.ExpiresAt != nil && value.ExpiresAt.After(created) {
		expires = value.ExpiresAt.UTC()
	}
	if expires.After(created.Add(24 * time.Hour)) {
		expires = created.Add(24 * time.Hour)
	}
	subject := domain.SignalSubjectV2{Type: "prefix", Value: value.SourcePrefix.Masked().String()}
	if !value.SourcePrefix.IsValid() {
		subject = domain.SignalSubjectV2{Type: "endpoint", Value: resourceID}
	}
	requested := legacyIntent(legacyDecision)
	state := domain.DecisionResolved
	reasons := []string{domain.ReasonCompatibilityObserveOnly, domain.ReasonCapabilityUnavailable}
	if !expires.After(now.UTC()) {
		state = domain.DecisionExpired
		reasons = append(reasons, domain.ReasonStale)
	}
	result := domain.ProtectionDecisionV2{Schema: domain.ProtectionDecisionSchemaV2, PolicyRevision: "legacy-score-v1", Subject: subject, Scope: domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: resourceID}, TargetResourceIDs: []string{resourceID}, SignalRefs: []string{}, SourceClasses: []string{"native"}, ScoreSnapshot: domain.ScoreSnapshotV2{Score: value.CurrentScore, TargetGroup: resourceID, CapturedAt: created}, ConfidenceBP: 0, ReasonCodes: reasons, RequestedIntent: requested, CreatedAt: created, ExpiresAt: expires, AllowlistResult: domain.PolicyCheckV2{Result: "unknown", ReasonCodes: []string{domain.ReasonUnknown}}, RecoveryResult: domain.PolicyCheckV2{Result: "unknown", ReasonCodes: []string{domain.ReasonUnknown}}, CapabilityResolution: domain.CapabilityResolutionV2{Implemented: false, ResolvedIntent: domain.IntentObserve, ReasonCodes: []string{domain.ReasonCapabilityUnavailable}}, State: state}
	result.FinalizeID()
	return result
}

func boundedResourceID(value string) string {
	value = strings.TrimSpace(value)
	if domain.ValidContractID(value, 256) {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return "legacy-resource:" + hex.EncodeToString(sum[:])
}

func legacyIntent(value string) domain.ResponseIntent {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "rate_limit":
		return domain.IntentRateLimit
	case "route_to_decoy":
		return domain.IntentRouteToDecoy
	case "block":
		return domain.IntentTemporaryBlock
	default:
		return domain.IntentObserve
	}
}
