package service

import (
	"strings"
	"testing"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	opsdiagnostics "github.com/MalenkiySolovey/solovey-ui/internal/ops/diagnostics"
)

func TestSummarizeDiagnosticChecks(t *testing.T) {
	healthy := opsdiagnostics.SummarizeChecks([]opsdiagnostics.Check{
		{Status: opsdiagnostics.StatusOK},
		{Status: opsdiagnostics.StatusOK},
	})
	if healthy.Status != "healthy" || healthy.Counts["ok"] != 2 {
		t.Fatalf("healthy summary = %#v", healthy)
	}

	degraded := opsdiagnostics.SummarizeChecks([]opsdiagnostics.Check{
		{Status: opsdiagnostics.StatusOK},
		{Status: opsdiagnostics.StatusWarn},
	})
	if degraded.Status != "degraded" || degraded.Counts["warn"] != 1 {
		t.Fatalf("degraded summary = %#v", degraded)
	}

	down := opsdiagnostics.SummarizeChecks([]opsdiagnostics.Check{
		{Status: opsdiagnostics.StatusWarn},
		{Status: opsdiagnostics.StatusFail},
	})
	if down.Status != "down" || down.Counts["fail"] != 1 {
		t.Fatalf("down summary = %#v", down)
	}
}

func TestDiagnosticsReportBuildsExpectedSections(t *testing.T) {
	initSettingTestDB(t)
	runtime := NewRuntime(coreruntime.NewCore())
	replaceDefaultRuntimeForTest(t, runtime)

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
	redacted := opsdiagnostics.RedactText(raw)
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

func hasDiagnosticCheck(checks []opsdiagnostics.Check, key string) bool {
	for _, check := range checks {
		if check.Key == key {
			return true
		}
	}
	return false
}
