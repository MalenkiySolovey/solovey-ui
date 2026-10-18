package secretref

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReferenceAllowsOnlyMaskedOwnerUseAndPresence(t *testing.T) {
	access := Access{Reference: NewReference("telegram", "telegramBotToken"), Actor: Actor{Component: "telegram"}, Operation: UseForOwner, IntentID: "fixture-intent"}
	if err := access.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Access){
		func(a *Access) { a.Actor.Component = "other" },
		func(a *Access) { a.Operation = "REVEAL" },
		func(a *Access) { a.Reference.Projection = "RAW" },
		func(a *Access) { a.Reference.Policy = "ARBITRARY" },
		func(a *Access) { a.IntentID = strings.Repeat("x", 97) },
	} {
		candidate := access
		mutate(&candidate)
		if candidate.Validate() != ErrDenied {
			t.Fatal("unauthorized secret access accepted")
		}
	}
	view := access.View(true, "deterministic fake plaintext")
	encoded, _ := json.Marshal(view)
	if view.RotationRevision != "" || strings.Contains(string(encoded), "plaintext") || view.Correlation.JobID == "" {
		t.Fatal("unsafe reference metadata")
	}
}
