package users

import (
	"context"
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"gorm.io/gorm"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func initSettingTestDB(tb testing.TB) {
	tb.Helper()
	if err := dbsqlite.Close(); err != nil {
		tb.Fatal(err)
	}
	if err := dbsqlite.InitializeContext(context.Background(), filepath.Join(tb.TempDir(), "panel.db"), dbsqlite.Bootstrap{
		BeforeOpen: func() error { return nil },
		AfterOpen:  func(db *gorm.DB) error { return db.AutoMigrate(&model.Client{}, &model.Changes{}) },
	}); err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := dbsqlite.Close(); err != nil {
			tb.Error(err)
		}
	})
}
func newLifecycle(notify func(int64)) Lifecycle { return Lifecycle{TouchLastUpdate: notify} }

func resetAccessClient(name, reason string, enabled bool, now int64) model.Client {
	return model.Client{Name: name, Enable: enabled, AccessDisableReason: reason,
		SubSecret: "fixture-" + name, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[1]`), Links: json.RawMessage(`[]`),
		AutoReset: true, NextReset: now - 1, ResetDays: 1, Up: 10, Down: 20, TotalUp: 100, TotalDown: 200, Volume: 300}
}

func TestScheduledResetPreservesStrongerAccessBlockers(t *testing.T) {
	initSettingTestDB(t)
	const now = int64(1_700_000_000)
	cases := []struct {
		name, reason, wantReason string
		enabled, wantEnabled     bool
		expiry                   int64
	}{
		{"manual", model.ClientDisabledManual, model.ClientDisabledManual, false, false, 0},
		{"legacy", model.ClientDisabledLegacy, model.ClientDisabledLegacy, false, false, 0},
		{"unclassified", "", "", false, false, 0},
		{"revoked", "CREDENTIAL_REVOKED", "CREDENTIAL_REVOKED", false, false, 0},
		{"future-blocker", "FUTURE_POLICY_BLOCK", "FUTURE_POLICY_BLOCK", false, false, 0},
		{"expired", model.ClientDisabledExpired, model.ClientDisabledExpired, false, false, now - 1},
		{"quota-and-expired", model.ClientDisabledQuota, model.ClientDisabledExpired, false, false, now - 1},
		{"quota-only", model.ClientDisabledQuota, "", false, true, now + 60},
		{"quota-unlimited-expiry", model.ClientDisabledQuota, "", false, true, 0},
		{"quota-at-expiry-boundary", model.ClientDisabledQuota, "", false, true, now},
		{"enabled", "", "", true, true, 0},
	}
	for _, tc := range cases {
		row := resetAccessClient(tc.name, tc.reason, tc.enabled, now)
		row.Expiry = tc.expiry
		if err := dbsqlite.DB().Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (&Lifecycle{}).ResetClients(dbsqlite.DB(), now); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		var row model.Client
		if err := dbsqlite.DB().Where("name = ?", tc.name).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.Enable != tc.wantEnabled || row.AccessDisableReason != tc.wantReason {
			t.Errorf("%s: enabled=%v reason=%q", tc.name, row.Enable, row.AccessDisableReason)
		}
		if row.Up != 0 || row.Down != 0 || row.TotalUp != 110 || row.TotalDown != 220 || row.NextReset != now+86400 {
			t.Errorf("%s: reset accounting/schedule changed: %#v", tc.name, row)
		}
		if row.Volume != 300 || row.Expiry != tc.expiry || row.SubSecret != "fixture-"+tc.name || string(row.Inbounds) != `[1]` {
			t.Errorf("%s: unrelated access identity changed", tc.name)
		}
	}
}

func TestDepleteClassifiesQuotaAndExpiryBeforeQuotaOnlyReset(t *testing.T) {
	initSettingTestDB(t)
	now := time.Now().Unix()
	quota := resetAccessClient("quota-cycle", "", true, now)
	quota.NextReset, quota.Up, quota.Volume = now+86400, 301, 300
	expired := resetAccessClient("expired-cycle", "", true, now)
	expired.NextReset, expired.Up, expired.Volume, expired.Expiry = now+86400, 301, 300, now-60
	for _, row := range []*model.Client{&quota, &expired} {
		if err := dbsqlite.DB().Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (&Lifecycle{}).DepleteClients(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		row    *model.Client
		reason string
	}{{&quota, model.ClientDisabledQuota}, {&expired, model.ClientDisabledExpired}} {
		var got model.Client
		if err := dbsqlite.DB().First(&got, tc.row.Id).Error; err != nil {
			t.Fatal(err)
		}
		if got.Enable || got.AccessDisableReason != tc.reason {
			t.Fatalf("depletion: %#v", got)
		}
		if err := dbsqlite.DB().Model(&model.Client{}).Where("id = ?", got.Id).Update("next_reset", now-1).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (&Lifecycle{}).ResetClients(dbsqlite.DB(), now+1); err != nil {
		t.Fatal(err)
	}
	var gotQuota, gotExpired model.Client
	if err := dbsqlite.DB().First(&gotQuota, quota.Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().First(&gotExpired, expired.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !gotQuota.Enable || gotQuota.AccessDisableReason != "" || gotExpired.Enable || gotExpired.AccessDisableReason != model.ClientDisabledExpired {
		t.Fatal("quota cycle reactivated a stronger expiry blocker or failed to clear quota-only state")
	}
}

func TestManualResetTrafficNeverGrantsAccess(t *testing.T) {
	initSettingTestDB(t)
	for _, reason := range []string{model.ClientDisabledManual, model.ClientDisabledQuota, model.ClientDisabledExpired, "CREDENTIAL_REVOKED", model.ClientDisabledLegacy} {
		row := resetAccessClient("manual-reset-"+reason, reason, false, 1_700_000_000)
		if err := dbsqlite.DB().Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := (&Lifecycle{}).ResetTraffic(strconv.FormatUint(uint64(row.Id), 10)); err != nil {
			t.Fatal(err)
		}
		var got model.Client
		if err := dbsqlite.DB().First(&got, row.Id).Error; err != nil {
			t.Fatal(err)
		}
		if got.Enable || got.AccessDisableReason != reason || got.Up != 0 || got.Down != 0 || got.TotalUp != 110 || got.TotalDown != 220 {
			t.Fatalf("manual reset altered access or accounting: %#v", got)
		}
	}
}

func TestResetClientsClampsZeroResetDays(t *testing.T) {
	initSettingTestDB(t)
	const now = int64(1_700_000_000)
	client := model.Client{
		Enable: true, Name: "zero-reset", Inbounds: json.RawMessage(`[1]`),
		Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`), AutoReset: true,
		NextReset: now - 1, ResetDays: 0, Up: 10, Down: 20,
	}
	if err := dbsqlite.DB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := (&Lifecycle{}).ResetClients(dbsqlite.DB(), now); err != nil {
		t.Fatal(err)
	}
	var got model.Client
	if err := dbsqlite.DB().First(&got, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if got.NextReset != now+86400 {
		t.Fatalf("resetDays=0 must clamp to one day: got %d want %d", got.NextReset, now+86400)
	}

	if err := dbsqlite.DB().Model(&model.Client{}).Where("id = ?", client.Id).Updates(map[string]interface{}{"up": 5, "down": 7}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (&Lifecycle{}).ResetClients(dbsqlite.DB(), now+120); err != nil {
		t.Fatal(err)
	}
	var after model.Client
	if err := dbsqlite.DB().First(&after, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if after.Up != 5 || after.Down != 7 {
		t.Fatalf("client was reset again too early: up=%d down=%d", after.Up, after.Down)
	}
}

func TestPeriodicResetPublishesCommittedChangeAndNoOpStaysQuiet(t *testing.T) {
	initSettingTestDB(t)
	now := time.Now().Unix()
	user := resetAccessClient("periodic-only", model.ClientDisabledQuota, false, now)
	if err := dbsqlite.DB().Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	owner := newLifecycle(func(stamp int64) {
		calls++
		var row model.Client
		if err := dbsqlite.DB().First(&row, user.Id).Error; err != nil {
			t.Fatal(err)
		}
		var changes int64
		if err := dbsqlite.DB().Model(&model.Changes{}).Where("actor = ? AND key = ? AND action = ?", "ResetJob", "clients", "reset").Count(&changes).Error; err != nil {
			t.Fatal(err)
		}
		if !row.Enable || row.Up != 0 || row.Down != 0 || row.TotalUp != 110 || row.TotalDown != 220 || changes != 1 || stamp < now {
			t.Fatal("freshness published before durable reset and journal")
		}
	})
	if _, err := owner.DepleteClients(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("periodic-only freshness calls=%d", calls)
	}
	if _, err := owner.DepleteClients(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("unchanged scheduler tick published another change")
	}
}

func TestPeriodicResetCommitFailureDoesNotPublishOrAdvancePeriod(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	user := resetAccessClient("periodic-failure", model.ClientDisabledManual, false, time.Now().Unix())
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE reset_commit_parent (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE reset_commit_child (parent_id INTEGER, FOREIGN KEY(parent_id) REFERENCES reset_commit_parent(id) DEFERRABLE INITIALLY DEFERRED)").Error; err != nil {
		t.Fatal(err)
	}
	callback := "test-deferred-reset-commit-failure"
	injected, calls := false, 0
	if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
		if injected || tx.Statement == nil || tx.Statement.Table != "changes" {
			return
		}
		injected = true
		if err := tx.Exec("INSERT INTO reset_commit_child(parent_id) VALUES (1)").Error; err != nil {
			_ = tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Create().Remove(callback) }()
	owner := newLifecycle(func(int64) { calls++ })
	if _, err := owner.DepleteClients(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Fatalf("wanted commit failure, got %v", err)
	}
	if !injected || calls != 0 {
		t.Fatal("failed reset published freshness")
	}
	var actual model.Client
	if err := db.First(&actual, user.Id).Error; err != nil {
		t.Fatal(err)
	}
	if actual.Up != user.Up || actual.Down != user.Down || actual.TotalUp != user.TotalUp || actual.NextReset != user.NextReset || actual.Enable || actual.AccessDisableReason != model.ClientDisabledManual {
		t.Fatal("failed reset committed counters, schedule or access state")
	}
	var changes int64
	if err := db.Model(&model.Changes{}).Where("actor = ?", "ResetJob").Count(&changes).Error; err != nil {
		t.Fatal(err)
	}
	if changes != 0 {
		t.Fatal("failed reset committed journal")
	}
}
