package cronjob

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deposist/s-ui-x/database"
	"github.com/deposist/s-ui-x/database/importxui"
	"github.com/deposist/s-ui-x/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestXUISyncJobExtraRunProfileSuccessRecordsSummary(t *testing.T) {
	initCronJobTestDB(t)
	profile := createXUISyncProfileForTest(t, createXUISyncSourceDB(t))
	job := &XUISyncJob{now: func() time.Time { return time.Unix(1700000000, 0) }}

	if err := job.RunProfile(context.Background(), profile); err != nil {
		t.Fatal(err)
	}

	var stored model.XUISyncProfile
	if err := database.GetDB().First(&stored, profile.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.LastRunStatus != "success" || stored.LastRunAt != 1700000000 {
		t.Fatalf("unexpected success run fields: %#v", stored)
	}
	var summary importxui.Report
	if err := json.Unmarshal(stored.LastRunSummary, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Summary.Inbounds.Total != 0 {
		t.Fatalf("unexpected success summary: %#v", summary.Summary)
	}
}

func TestXUISyncJobExtraRunProfileFailureRecordsFailedSummary(t *testing.T) {
	initCronJobTestDB(t)
	profile := createXUISyncProfileForTest(t, filepath.Join(t.TempDir(), "missing.db"))
	job := &XUISyncJob{now: func() time.Time { return time.Unix(1700000100, 0) }}

	if err := job.RunProfile(context.Background(), profile); err == nil {
		t.Fatal("missing source should fail")
	}

	var stored model.XUISyncProfile
	if err := database.GetDB().First(&stored, profile.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.LastRunStatus != "failed" || stored.LastRunAt != 1700000100 {
		t.Fatalf("unexpected failed run fields: %#v", stored)
	}
	var summary map[string]any
	if err := json.Unmarshal(stored.LastRunSummary, &summary); err != nil {
		t.Fatal(err)
	}
	if summary["error"] != "failed" {
		t.Fatalf("current baseline should persist generic failed summary: %#v", summary)
	}
}

func TestXUISyncJobExtraMinIntervalBlocksRecentRun(t *testing.T) {
	job := &XUISyncJob{now: func() time.Time { return time.Unix(1700000000, 0) }}
	profile := &model.XUISyncProfile{Id: 42, LastRunAt: time.Unix(1700000000, 0).Add(-time.Minute).Unix()}

	err := job.RunProfile(context.Background(), profile)
	if err == nil || !strings.Contains(err.Error(), "too recently") {
		t.Fatalf("expected min-interval error, got %v", err)
	}
}

func TestXUISyncJobExtraContextCancelDuringRetry(t *testing.T) {
	initCronJobTestDB(t)
	profile := createXUISyncProfileForTest(t, filepath.Join(t.TempDir(), "missing.db"))
	job := &XUISyncJob{now: func() time.Time { return time.Unix(1700000200, 0) }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := job.RunProfile(ctx, profile)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestXUISyncJobExtraFailureSummaryKeepsLastErr(t *testing.T) {
	initCronJobTestDB(t)
	profile := createXUISyncProfileForTest(t, filepath.Join(t.TempDir(), "missing.db"))
	job := &XUISyncJob{now: func() time.Time { return time.Unix(1700000300, 0) }}
	_ = job.RunProfile(context.Background(), profile)

	var stored model.XUISyncProfile
	if err := database.GetDB().First(&stored, profile.Id).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored.LastRunSummary) == 0 {
		t.Fatal("last_run_summary should remain non-empty for failed sync")
	}
	var audit model.AuditEvent
	if err := database.GetDB().Where("event = ?", "xui_sync_failed").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	var details map[string]any
	if err := json.Unmarshal(audit.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details["errorClass"] != "source" {
		t.Fatalf("xui_sync_failed audit should include source errorClass, got %#v", details)
	}
}

func TestXUISyncJobExtraFailureSummaryIncludesLastErr_XFAILIssue4(t *testing.T) {
	t.Skip("XFAIL: issue 4; xui sync failure summary currently stores generic error=failed instead of lastErr")

	initCronJobTestDB(t)
	profile := createXUISyncProfileForTest(t, filepath.Join(t.TempDir(), "missing.db"))
	job := &XUISyncJob{now: func() time.Time { return time.Unix(1700000300, 0) }}
	_ = job.RunProfile(context.Background(), profile)

	var stored model.XUISyncProfile
	if err := database.GetDB().First(&stored, profile.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored.LastRunSummary), "missing.db") {
		t.Fatalf("last_run_summary should include real lastErr, got %s", stored.LastRunSummary)
	}
}

func createXUISyncProfileForTest(t *testing.T, sourcePath string) *model.XUISyncProfile {
	t.Helper()
	profile, err := importxui.SaveSyncProfile(importxui.SyncProfileInput{
		Name:       "phase2",
		SourceType: "file",
		Source: importxui.SyncProfileSource{
			Type: "file",
			URL:  sourcePath,
		},
		Strategy: importxui.StrategyMerge,
		OnlyNew:  true,
		Enabled:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func createXUISyncSourceDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x-ui.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer sqlDB.Close()
	}
	if err := db.Exec(`CREATE TABLE inbounds (
		id integer primary key,
		user_id integer,
		up integer,
		down integer,
		total integer,
		all_time integer,
		remark text,
		enable integer,
		expiry_time integer,
		traffic_reset text,
		last_traffic_reset_time integer,
		listen text,
		port integer,
		protocol text,
		settings text,
		stream_settings text,
		tag text,
		sniffing text
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE client_traffics (
		id integer primary key,
		inbound_id integer,
		enable integer,
		email text,
		up integer,
		down integer,
		all_time integer,
		expiry_time integer,
		total integer,
		reset integer,
		last_online integer
	)`).Error; err != nil {
		t.Fatal(err)
	}
	return path
}
