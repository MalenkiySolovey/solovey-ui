package importxui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeXUIApplyPlanMapsOversizeFileDecodeError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := decodeApplyPlan(&Upload{
		PlanPath: path,
		PlanSize: MaxFieldBytes + 1,
		Fields:   map[string]string{},
	})
	if err == nil {
		t.Fatal("expected decode error")
	}
	var tooLarge *xuiFieldTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("error type=%T, want *xuiFieldTooLargeError", err)
	}
	if tooLarge.Field != "plan" || tooLarge.Limit != MaxFieldBytes {
		t.Fatalf("too-large error=%#v", tooLarge)
	}
}

func TestDecodeXUIApplyPlanReadsInlineField(t *testing.T) {
	plan, err := decodeApplyPlan(&Upload{
		Fields: map[string]string{
			"plan": `{"source":{"hash":"inline-hash"}}`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Source.Hash != "inline-hash" {
		t.Fatalf("plan source hash=%q, want inline-hash", plan.Source.Hash)
	}
}
