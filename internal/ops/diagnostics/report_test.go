package diagnostics

import (
	"strings"
	"testing"
)

func TestSummarizeChecks(t *testing.T) {
	report := SummarizeChecks([]Check{
		{Status: StatusOK},
		{Status: StatusWarn},
	})
	if report.Status != "degraded" || report.Counts["warn"] != 1 {
		t.Fatalf("summary = %#v", report)
	}
}

func TestRedactText(t *testing.T) {
	raw := `Authorization: Bearer secret-token password="admin" https://example.test/?token=raw`
	redacted := RedactText(raw)
	for _, forbidden := range []string{"secret-token", "admin", "token=raw"} {
		if strings.Contains(redacted, forbidden) {
			t.Fatalf("redaction leaked %q in %s", forbidden, redacted)
		}
	}
}

func TestNaiveSupportCheck(t *testing.T) {
	raw := []byte(`{"outbounds":[{"type":"naive","tag":"n1"}]}`)
	checks := NaiveSupportCheck(raw, false)
	if len(checks) != 1 || checks[0].Status != StatusWarn {
		t.Fatalf("expected naive warning, got %#v", checks)
	}
	if checks := NaiveSupportCheck(raw, true); len(checks) != 0 {
		t.Fatalf("supported naive should not warn: %#v", checks)
	}
}
