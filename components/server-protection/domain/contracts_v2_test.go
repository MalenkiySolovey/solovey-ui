package domain

import (
	"strings"
	"testing"
	"time"
)

func validSignalV2(now time.Time) ProtectionSignalV2 {
	value := ProtectionSignalV2{Schema: ProtectionSignalSchemaV2, Source: SignalSourceV2{SourceID: "fixture", Producer: "fixture", ProducerVersion: "v1", TrustClass: "native", SourceClass: "native"}, Category: SignalCategoryEndpointObservation, Kind: "unknown_future_kind", KnownKind: false, Subject: SignalSubjectV2{Type: "ip", Value: "192.0.2.1"}, Scope: SignalScopeV2{Scope: ScopeEndpoint, TargetResourceID: "core:inbound:1"}, ObservedAt: now, ExpiresAt: now.Add(time.Hour), ConfidenceBP: 5000, SafeMeta: map[string]string{"method_class": "get"}, Provenance: SignalProvenanceV2{AdapterID: "fixture", SourceRevision: "source-1", PolicyRevision: "policy-1"}}
	value.FinalizeID("event-1")
	return value
}

func TestSignalV2UnknownKindIsVisibleBoundedAndNonSecret(t *testing.T) {
	now := time.Now().UTC()
	value := validSignalV2(now)
	if err := value.Validate(now); err != nil {
		t.Fatal(err)
	}
	if value.KnownKind {
		t.Fatal("unknown kind became known")
	}
	value.KnownKind = true
	if value.Validate(now) == nil {
		t.Fatal("unknown kind was accepted as a supported versioned enum value")
	}
	value = validSignalV2(now)
	value.SafeMeta["header_dump"] = strings.Repeat("x", 257)
	if value.Validate(now) == nil {
		t.Fatal("oversize safe metadata accepted")
	}
	value = validSignalV2(now)
	value.ExpiresAt = value.ObservedAt.Add(MaxSignalLifetime + time.Second)
	if value.Validate(now) == nil {
		t.Fatal("overlong signal lifetime accepted")
	}
}

func TestSignalIDIsIdempotentAndScoped(t *testing.T) {
	now := time.Now().UTC()
	left := validSignalV2(now)
	right := validSignalV2(now)
	if left.SignalID != right.SignalID {
		t.Fatal("same event normalized to different ids")
	}
	right.Scope.TargetResourceID = "core:inbound:2"
	right.FinalizeID("event-1")
	if left.SignalID == right.SignalID {
		t.Fatal("scope did not affect signal id")
	}
}

func TestSignalV2RejectsCrossScopeEscalationAndPathIdentifiers(t *testing.T) {
	now := time.Now().UTC()
	value := validSignalV2(now)
	value.Category = SignalCategoryPanelAuth
	value.Scope = SignalScopeV2{Scope: ScopeHostWide}
	value.FinalizeID("event-cross-scope")
	if value.Validate(now) == nil {
		t.Fatal("panel-auth signal escalated to host-wide scope")
	}
	value = validSignalV2(now)
	value.Scope.TargetResourceID = `/var/lib/private/token`
	value.FinalizeID("event-path")
	if value.Validate(now) == nil {
		t.Fatal("path-shaped resource identifier accepted")
	}
}

func TestSignalV2RejectsOpaqueTargetOnSemanticScope(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	value := validSignalV2(now)
	value.Category = SignalCategoryPanelAuth
	value.Scope = SignalScopeV2{Scope: ScopePanelAuth, TargetResourceID: "core:inbound:1"}
	value.FinalizeID("panel-auth-target")
	if value.Validate(now) == nil {
		t.Fatal("PANEL_AUTH signal accepted an opaque inbound target without a trusted mapping")
	}
}

func TestDecisionV2SeparatesCapabilityFromAppliedAction(t *testing.T) {
	now := time.Now().UTC()
	value := ProtectionDecisionV2{Schema: ProtectionDecisionSchemaV2, PolicyRevision: "policy-1", Subject: SignalSubjectV2{Type: "ip", Value: "192.0.2.1"}, Scope: SignalScopeV2{Scope: ScopeEndpoint, TargetResourceID: "core:inbound:1"}, TargetResourceIDs: []string{"core:inbound:1"}, SignalRefs: []string{strings.Repeat("a", 64)}, SourceClasses: []string{"native"}, ScoreSnapshot: ScoreSnapshotV2{Score: 10, TargetGroup: "core:inbound:1", CapturedAt: now}, ConfidenceBP: 5000, ReasonCodes: []string{ReasonCapabilityUnavailable}, RequestedIntent: IntentRouteToDecoy, CreatedAt: now, ExpiresAt: now.Add(time.Hour), AllowlistResult: PolicyCheckV2{Result: "unknown"}, RecoveryResult: PolicyCheckV2{Result: "unknown"}, CapabilityResolution: CapabilityResolutionV2{Implemented: false, ResolvedIntent: IntentObserve, ReasonCodes: []string{ReasonCapabilityUnavailable}}, State: DecisionResolved}
	value.FinalizeID()
	if err := value.Validate(now); err != nil {
		t.Fatal(err)
	}
	forged := value
	forged.AllowlistResult.Result = "allow"
	if forged.Validate(now) == nil {
		t.Fatal("decision accepted changed policy evidence under a stale DecisionID")
	}
	value.State = DecisionApplied
	if value.Validate(now) == nil {
		t.Fatal("decision without AppliedActionV1 was accepted as applied")
	}
	value.State = DecisionResolved
	value.CapabilityResolution.Implemented = true
	value.CapabilityResolution.ResolvedIntent = IntentTemporaryBlock
	if err := value.Validate(now); err != nil {
		t.Fatalf("firewall baseline endpoint capability should be valid without claiming APPLIED: %v", err)
	}
	value.CapabilityResolution.Implemented = false
	if value.Validate(now) == nil {
		t.Fatal("unavailable capability resolved to a non-observe intent")
	}
	value.CapabilityResolution.ResolvedIntent = IntentObserve
	value.TargetResourceIDs = []string{"core:inbound:2"}
	if value.Validate(now) == nil {
		t.Fatal("decision target crossed its declared endpoint scope")
	}
}

func TestDecisionV2RejectsStaleScoreSnapshot(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	value := ProtectionDecisionV2{Schema: ProtectionDecisionSchemaV2, PolicyRevision: "policy-1", Subject: SignalSubjectV2{Type: "ip", Value: "192.0.2.1"}, Scope: SignalScopeV2{Scope: ScopeEndpoint, TargetResourceID: "core:inbound:1"}, TargetResourceIDs: []string{"core:inbound:1"}, SignalRefs: []string{strings.Repeat("a", 64)}, SourceClasses: []string{"native"}, ScoreSnapshot: ScoreSnapshotV2{Score: 10, TargetGroup: "core:inbound:1", CapturedAt: now.Add(-6 * time.Minute)}, ConfidenceBP: 5000, ReasonCodes: []string{ReasonCapabilityUnavailable}, RequestedIntent: IntentObserve, CreatedAt: now, ExpiresAt: now.Add(time.Hour), AllowlistResult: PolicyCheckV2{Result: "unknown"}, RecoveryResult: PolicyCheckV2{Result: "unknown"}, CapabilityResolution: CapabilityResolutionV2{Implemented: false, ResolvedIntent: IntentObserve, ReasonCodes: []string{ReasonCapabilityUnavailable}}, State: DecisionResolved}
	value.FinalizeID()
	if value.Validate(now) == nil {
		t.Fatal("decision accepted a stale score snapshot as current policy input")
	}
}

func TestDecisionV2RejectsCrossScopeTargetGroup(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	value := ProtectionDecisionV2{Schema: ProtectionDecisionSchemaV2, PolicyRevision: "policy-1", Subject: SignalSubjectV2{Type: "account_pseudonym", Value: "account:" + strings.Repeat("b", 64)}, Scope: SignalScopeV2{Scope: ScopePanelAuth}, SignalRefs: []string{strings.Repeat("a", 64)}, SourceClasses: []string{"native"}, ScoreSnapshot: ScoreSnapshotV2{Score: 10, TargetGroup: "core:inbound:1", CapturedAt: now}, ConfidenceBP: 5000, ReasonCodes: []string{ReasonCapabilityUnavailable}, RequestedIntent: IntentObserve, CreatedAt: now, ExpiresAt: now.Add(time.Hour), AllowlistResult: PolicyCheckV2{Result: "unknown"}, RecoveryResult: PolicyCheckV2{Result: "unknown"}, CapabilityResolution: CapabilityResolutionV2{Implemented: false, ResolvedIntent: IntentObserve, ReasonCodes: []string{ReasonCapabilityUnavailable}}, State: DecisionResolved}
	value.FinalizeID()
	if value.Validate(now) == nil {
		t.Fatal("PANEL_AUTH decision accepted an inbound target group")
	}
}
