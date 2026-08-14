//go:build !minimal

package importxui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestDecodeXUIApplyPlanRejectsUnknownBrowserFields(t *testing.T) {
	_, err := decodeXUIApplyPlanReader(strings.NewReader(`{
		"items":[{"kind":"inbound","srcId":1,"dstTag":"inbound-1","action":"create","conflict":false,"previewJson":{},"rowKey":"ui-only"}],
		"defaults":{"strategy":"merge","includeSettings":false,"adminMode":"skip","onlyNew":false,"includeHistory":false,"includeRouting":false},
		"source":{"hash":"hash"}
	}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown-field rejection, got %v", err)
	}
}

func TestDecodeXUIApplyPlanRejectsTrailingDocument(t *testing.T) {
	_, err := decodeXUIApplyPlanReader(strings.NewReader(`{"source":{"hash":"one"}} {"source":{"hash":"two"}}`))
	if err == nil || !strings.Contains(err.Error(), "multiple migration plans") {
		t.Fatalf("expected multiple-document rejection, got %v", err)
	}
}
