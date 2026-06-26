package doctor

import (
	"testing"
	"time"
)

func TestFinishReportSummarizesSeverity(t *testing.T) {
	report := FinishReport(time.Now(), []Item{
		OK("a", "A", "ok", nil),
		Warn("b", "B", "warn", "fix", nil),
	})
	if report.Status != SeverityWarn || report.Summary != "1 warning(s)" {
		t.Fatalf("warning report = %#v", report)
	}

	report = FinishReport(time.Now(), []Item{
		Warn("b", "B", "warn", "fix", nil),
		Error("c", "C", "error", "fix", nil),
	})
	if report.Status != SeverityError || report.Summary != "1 error(s), 1 warning(s)" {
		t.Fatalf("error report = %#v", report)
	}
}
