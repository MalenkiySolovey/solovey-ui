package service

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core"
)

func TestSummarizeDiagnosticChecks(t *testing.T) {
	healthy := summarizeDiagnosticChecks([]DiagnosticCheck{
		{Status: DiagnosticStatusOK},
		{Status: DiagnosticStatusOK},
	})
	if healthy.Status != "healthy" || healthy.Counts["ok"] != 2 {
		t.Fatalf("healthy summary = %#v", healthy)
	}

	degraded := summarizeDiagnosticChecks([]DiagnosticCheck{
		{Status: DiagnosticStatusOK},
		{Status: DiagnosticStatusWarn},
	})
	if degraded.Status != "degraded" || degraded.Counts["warn"] != 1 {
		t.Fatalf("degraded summary = %#v", degraded)
	}

	down := summarizeDiagnosticChecks([]DiagnosticCheck{
		{Status: DiagnosticStatusWarn},
		{Status: DiagnosticStatusFail},
	})
	if down.Status != "down" || down.Counts["fail"] != 1 {
		t.Fatalf("down summary = %#v", down)
	}
}

func TestDiagnosticsReportBuildsExpectedSections(t *testing.T) {
	initSettingTestDB(t)
	runtime := NewRuntime(core.NewCore())
	restore := ReplaceDefaultRuntimeForTest(runtime)
	defer restore()

	report := (&DiagnosticsService{Runtime: runtime}).Report()
	if report.GeneratedAt == 0 {
		t.Fatal("generatedAt is empty")
	}
	if len(report.Checks) == 0 {
		t.Fatal("diagnostic checks are empty")
	}
	if report.Health.Counts["fail"] == 0 {
		t.Fatalf("core is not started in this test, so the report should surface at least one failure: %#v", report.Health)
	}
	if report.Database == nil || report.Settings == nil || report.Logs == nil || report.System == nil {
		t.Fatalf("report sections missing: %#v", report)
	}
	if _, ok := report.Settings["webPort"]; !ok {
		t.Fatalf("settings snapshot missing webPort: %#v", report.Settings)
	}
	if !hasDiagnosticCheck(report.Checks, "config_parse") {
		t.Fatalf("report missing config_parse check: %#v", report.Checks)
	}
}

func TestRedactDiagnosticText(t *testing.T) {
	raw := `Authorization: Bearer core-secret-token password="admin-pass" subToken=abc123 https://example.test/?token=raw&ok=1`
	redacted := redactDiagnosticText(raw)
	for _, forbidden := range []string{"core-secret-token", "admin-pass", "abc123", "token=raw"} {
		if redacted == raw || strings.Contains(redacted, forbidden) {
			t.Fatalf("diagnostic redaction leaked %q: %s", forbidden, redacted)
		}
	}
	for _, expected := range []string{"Authorization: Bearer [redacted]", `password=[redacted]`, "subToken=[redacted]", "token=[redacted]"} {
		if !strings.Contains(redacted, expected) {
			t.Fatalf("diagnostic redaction missing %q: %s", expected, redacted)
		}
	}
}

func hasDiagnosticCheck(checks []DiagnosticCheck, key string) bool {
	for _, check := range checks {
		if check.Key == key {
			return true
		}
	}
	return false
}
