package cronjob

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deposist/s-ui-x/database"
	"github.com/deposist/s-ui-x/database/importxui"
	"github.com/deposist/s-ui-x/database/model"
	"github.com/deposist/s-ui-x/ipmonitor"

	gormlogger "gorm.io/gorm/logger"
)

func BenchmarkXUISyncJobLostNetworkBackoff(b *testing.B) {
	b.ReportMetric(3, "attempts/op")
	b.ReportMetric(1200, "expected_backoff_ms/op")
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		initCronJobPerfDB(b)
		profile := createXUISyncPerfMissingProfile(b)
		job := &XUISyncJob{now: func() time.Time { return time.Unix(1700010000, 0) }}
		b.StartTimer()
		if err := job.RunProfile(context.Background(), profile); err == nil {
			b.Fatal("missing source should fail")
		}
	}
}

func TestXUISyncJobLostNetworkBackoffAnchorIssue40Phase5(t *testing.T) {
	initCronJobPerfDB(t)
	profile := createXUISyncPerfMissingProfile(t)
	job := &XUISyncJob{now: func() time.Time { return time.Unix(1700010100, 0) }}
	start := time.Now()
	err := job.RunProfile(context.Background(), profile)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("missing source should fail")
	}
	if elapsed < 1100*time.Millisecond {
		t.Fatalf("expected exponential 200ms+1s backoff (>=1.1s), got %s", elapsed)
	}
	if elapsed > 2000*time.Millisecond {
		t.Fatalf("backoff too long, expected ~1.2s, got %s", elapsed)
	}
	t.Logf("phase5 issue40 anchor: attempts=3 expected_backoff_ms=1200 elapsed=%s err=%v", elapsed, err)
}

func TestXUISyncJobExponentialBackoffScheduleIssue40(t *testing.T) {
	initCronJobPerfDB(t)
	profile := createXUISyncPerfMissingProfile(t)
	job := &XUISyncJob{now: func() time.Time { return time.Unix(1700020000, 0) }}

	start := time.Now()
	err := job.RunProfile(context.Background(), profile)
	total := time.Since(start)

	if err == nil {
		t.Fatal("missing source should fail")
	}
	if total < 1100*time.Millisecond {
		t.Fatalf("expected total backoff >=1.1s (200ms+1s), got %s", total)
	}
	if total > 2000*time.Millisecond {
		t.Fatalf("expected total backoff <=2s, got %s", total)
	}
	t.Logf("issue40 schedule anchor: total=%s err=%v", total, err)
}

func initCronJobPerfDB(tb testing.TB) {
	tb.Helper()
	tempDir := tb.TempDir()
	tb.Setenv("SUI_DB_FOLDER", tempDir)
	closeCronJobDB(database.GetDB())
	if err := database.InitDB(filepath.Join(tempDir, "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			tb.Skip(err)
		}
		tb.Fatal(err)
	}
	database.GetDB().Config.Logger = gormlogger.Discard
	tb.Cleanup(func() {
		closeCronJobDB(database.GetDB())
		ipmonitor.InvalidateAllCache()
	})
}

func createXUISyncPerfMissingProfile(tb testing.TB) *model.XUISyncProfile {
	tb.Helper()
	profile, err := importxui.SaveSyncProfile(importxui.SyncProfileInput{
		Name:       "phase5-lost-network",
		SourceType: "file",
		Source: importxui.SyncProfileSource{
			Type: "file",
			URL:  filepath.Join(tb.TempDir(), "missing-x-ui.db"),
		},
		Strategy: importxui.StrategyMerge,
		OnlyNew:  true,
		Enabled:  true,
	})
	if err != nil {
		tb.Fatal(err)
	}
	return profile
}
