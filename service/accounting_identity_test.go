package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	coretracker "github.com/MalenkiySolovey/solovey-ui/core/tracker"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	clientsvc "github.com/MalenkiySolovey/solovey-ui/service/client"
	"gorm.io/gorm"
)

func TestDurableAccountingSurvivesCanonicalRenameAndTokenRotation(t *testing.T) {
	initSettingTestDB(t)
	user := model.Client{Name: "original", Enable: true, SubSecret: "retrieval-only", Config: json.RawMessage(`{"vless":{"uuid":"credential-fixture"}}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Up: 20, Down: 30, TotalUp: 100, TotalDown: 200, Volume: 1000}
	if err := dbsqlite.DB().Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	samples := []coretracker.Stat{{DateTime: time.Now().Unix(), Resource: "user", Tag: user.Name, VPNUserID: user.VPNUserID(), Direction: true, Traffic: 11}, {DateTime: time.Now().Unix(), Resource: "user", Tag: user.Name, VPNUserID: user.VPNUserID(), Direction: false, Traffic: 13}}
	user.Name = "renamed"
	payload, _ := json.Marshal(user)
	if err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error { _, err := (&ClientService{}).Save(tx, "edit", payload, ""); return err }); err != nil {
		t.Fatal(err)
	}
	other := model.Client{Name: "original", Enable: true, SubSecret: "different-user", Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)}
	if err := dbsqlite.DB().Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (&ClientService{}).RotateSubSecret(strconv.FormatUint(uint64(user.Id), 10)); err != nil {
		t.Fatal(err)
	}
	if err := (&StatsService{}).saveStatsSamples(true, samples); err != nil {
		t.Fatal(err)
	}
	var actual, unrelated model.Client
	if err := dbsqlite.DB().First(&actual, user.Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().First(&unrelated, other.Id).Error; err != nil {
		t.Fatal(err)
	}
	if actual.VPNUserID() != user.VPNUserID() || actual.Name != "renamed" || actual.Up != 31 || actual.Down != 43 || actual.TotalUp != 100 || actual.TotalDown != 200 || actual.Volume != 1000 {
		t.Fatal("rename/token rotation detached durable accounting")
	}
	if string(actual.Config) != string(user.Config) || !actual.Enable || actual.SubSecret == user.SubSecret {
		t.Fatal("retrieval token rotation changed access or protocol credential")
	}
	if unrelated.Up != 0 || unrelated.Down != 0 {
		t.Fatal("reused display name inherited someone else's traffic")
	}
	// A name supplied without the accepted runtime binding remains observation-
	// only, even if it matches a current user exactly.
	if err := (&StatsService{}).saveStatsSamples(false, []coretracker.Stat{{Resource: "user", Tag: other.Name, Direction: true, Traffic: 999}}); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().First(&unrelated, other.Id).Error; err != nil {
		t.Fatal(err)
	}
	if unrelated.Up != 0 {
		t.Fatal("unproven runtime label became accounting identity")
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
	owner := clientsvc.New(func(stamp int64) {
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
	owner := clientsvc.New(func(int64) { calls++ })
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
