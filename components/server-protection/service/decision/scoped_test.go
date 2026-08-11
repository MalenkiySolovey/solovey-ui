package decision

import (
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
)

func TestScopedDecisionMediumConfidenceUsesGrayOrRateNotAutomaticDrop(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	signal := scopedSignal(now, 6000)
	decision, err := ResolveScopedDecision(ScopedDecisionInput{Subject: signal.Subject, Scope: signal.Scope, Signals: []domain.ProtectionSignalV2{signal}, Score: domain.ScoreSnapshotV2{Score: 100, TargetGroup: signal.Scope.TargetResourceID, CapturedAt: now}, AllowlistResult: domain.PolicyCheckV2{Result: "deny"}, RecoveryResult: domain.PolicyCheckV2{Result: "allow"}, RevisionBinding: scopedRevisionBinding(), Policy: DefaultEscalationPolicyV1(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if decision.RequestedIntent != domain.IntentRateLimit || decision.State != domain.DecisionCandidate || decision.CapabilityResolution.Implemented || decision.CapabilityResolution.ResolvedIntent != domain.IntentObserve {
		t.Fatalf("medium-confidence decision became an automatic drop or action: %#v", decision)
	}
}

func TestScopedDecisionRejectsCrossScopeAndHonorsRecoveryPrecedence(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	signal := scopedSignal(now, 9000)
	other := signal
	other.Scope.TargetResourceID = "core:inbound:other"
	other.FinalizeID("other")
	if _, err := ResolveScopedDecision(ScopedDecisionInput{Subject: signal.Subject, Scope: signal.Scope, Signals: []domain.ProtectionSignalV2{signal, other}, Score: domain.ScoreSnapshotV2{Score: 100, TargetGroup: signal.Scope.TargetResourceID, CapturedAt: now}, AllowlistResult: domain.PolicyCheckV2{Result: "deny"}, RecoveryResult: domain.PolicyCheckV2{Result: "deny"}, RevisionBinding: scopedRevisionBinding(), Policy: DefaultEscalationPolicyV1(), Now: now}); err == nil {
		t.Fatal("cross-scope signals were combined")
	}
	decision, err := ResolveScopedDecision(ScopedDecisionInput{Subject: signal.Subject, Scope: signal.Scope, Signals: []domain.ProtectionSignalV2{signal}, Score: domain.ScoreSnapshotV2{Score: 100, TargetGroup: signal.Scope.TargetResourceID, CapturedAt: now}, AllowlistResult: domain.PolicyCheckV2{Result: "deny"}, RecoveryResult: domain.PolicyCheckV2{Result: "deny"}, RevisionBinding: scopedRevisionBinding(), Policy: DefaultEscalationPolicyV1(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if decision.RequestedIntent != domain.IntentObserve || !containsDecisionReason(decision.ReasonCodes, "recovery_precedence") {
		t.Fatalf("recovery precedence did not win: %#v", decision)
	}
}

func scopedRevisionBinding() domain.DecisionRevisionBindingV2 {
	return domain.DecisionRevisionBindingV2{
		StrategyRevision: "strategy-revision-v2", CapabilityRevision: "capability-revision-v2",
		ActionScopeRevision: strings.Repeat("a", 64), EndpointRevision: strings.Repeat("b", 64),
		ResourceRevision: strings.Repeat("c", 64), ConfigurationRevision: strings.Repeat("d", 64),
	}
}

func scopedSignal(now time.Time, confidence int) domain.ProtectionSignalV2 {
	signal := domain.ProtectionSignalV2{Schema: domain.ProtectionSignalSchemaV2, Source: domain.SignalSourceV2{SourceID: "fixture", Producer: "fixture", ProducerVersion: "v1", TrustClass: "native", SourceClass: "native"}, Category: domain.SignalCategoryConnectionMetadata, Kind: string(domain.SignalFallbackHit), KnownKind: true, Subject: domain.SignalSubjectV2{Type: "ip", Value: "192.0.2.10"}, Scope: domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: "core:inbound:one", EndpointID: "endpoint:one", Transport: "tcp"}, ObservedAt: now, ExpiresAt: now.Add(time.Hour), ConfidenceBP: confidence, Provenance: domain.SignalProvenanceV2{AdapterID: "fixture", SourceRevision: "source-1", PolicyRevision: "firewallBaseline-policy", ObservationWindowID: "window:one"}}
	signal.FinalizeID(strings.Repeat("a", 16))
	return signal
}

func containsDecisionReason(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
