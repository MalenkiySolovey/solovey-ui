package domain

import (
	"strings"
	"testing"
	"time"
)

func TestAppliedActionRequiresVerifiedExactActualRevision(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	action := AppliedActionV1{Schema: AppliedActionSchemaV1, DecisionID: strings.Repeat("a", 64), PlanDigest: strings.Repeat("b", 64), ResourceID: "core:inbound:one", Subject: SignalSubjectV2{Type: "ip", Value: "192.0.2.10"}, GraphRevision: strings.Repeat("9", 64), EndpointRevision: strings.Repeat("c", 64), ResourceRevision: strings.Repeat("d", 64), ConfigurationRevision: strings.Repeat("e", 64), RequestedIntent: IntentTemporaryBlock, ResolvedIntent: IntentTemporaryBlock, DesiredState: "REQUESTED", SelectedState: "SELECTED", ActualState: "NOT_APPLIED", State: ActionPlanned, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	action.FinalizeID()
	for name, mutate := range map[string]func(*AppliedActionV1){
		"configuration-revision": func(value *AppliedActionV1) { value.ConfigurationRevision = strings.Repeat("0", 64) },
		"subject":                func(value *AppliedActionV1) { value.Subject.Value = "192.0.2.11" },
		"expiry":                 func(value *AppliedActionV1) { value.ExpiresAt = value.ExpiresAt.Add(time.Minute) },
	} {
		t.Run("identity-"+name, func(t *testing.T) {
			forged := action
			mutate(&forged)
			if forged.Validate(now) == nil {
				t.Fatal("action accepted immutable contract mutation under a stale ActionID")
			}
		})
	}
	if _, err := action.MarkApplied(strings.Repeat("f", 64), now.Add(time.Minute)); err == nil {
		t.Fatal("planned action skipped verified state and claimed APPLIED")
	}
	verified, err := action.MarkVerified(strings.Repeat("f", 64), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if verified.State != ActionVerified || verified.ActualState != "VERIFIED_NOT_APPLIED" {
		t.Fatalf("verification false-claimed apply: %#v", verified)
	}
	if _, err := verified.MarkApplied(strings.Repeat("0", 64), now.Add(2*time.Minute)); err == nil {
		t.Fatal("mismatched actual revision was accepted")
	}
	applied, err := verified.MarkApplied(strings.Repeat("f", 64), now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if applied.State != ActionApplied || applied.ActualState != "APPLIED" || applied.ActualRevision != strings.Repeat("f", 64) {
		t.Fatalf("verified exact action did not become applied: %#v", applied)
	}
	if err := applied.Validate(now.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
}
