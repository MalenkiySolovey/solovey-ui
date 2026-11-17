package accounting

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/backend/accounting/ipmonitor"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"gorm.io/gorm"
)

func TestAccountingStableIdentityPublishesOnlyCommittedCounters(t *testing.T) {
	initSettingTestDB(t)
	bound := model.Client{Name: "renamed", Enable: true, Up: 20, Down: 30}
	reused := model.Client{Name: "original", Enable: true}
	if err := dbsqlite.DB().Create(&bound).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&reused).Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	owner := Recorder{Publish: func(online OnlineResources, rows []model.Stats) {
		calls++
		var actual model.Client
		if err := dbsqlite.DB().First(&actual, bound.Id).Error; err != nil {
			t.Fatal(err)
		}
		if actual.Up != 31 || actual.Down != 43 || len(rows) != 3 {
			t.Fatal("publication preceded the durable accounting commit")
		}
	}}
	samples := []Sample{
		{VPNUserID: bound.VPNUserID(), Resource: "user", Tag: "original", Direction: true, Traffic: 11},
		{VPNUserID: bound.VPNUserID(), Resource: "user", Tag: "original", Direction: false, Traffic: 13},
		{Resource: "user", Tag: "original", Direction: true, Traffic: 999},
	}
	if err := owner.PersistSamples(false, samples); err != nil {
		t.Fatal(err)
	}
	var other model.Client
	if err := dbsqlite.DB().First(&other, reused.Id).Error; err != nil {
		t.Fatal(err)
	}
	var history int64
	if err := dbsqlite.DB().Model(&model.Stats{}).Count(&history).Error; err != nil {
		t.Fatal(err)
	}
	if calls != 1 || other.Up != 0 || other.Down != 0 || history != 0 {
		t.Fatal("unbound labels acquired durable identity or disabled history was written")
	}
}

func TestAccountingCommitFailureRequeuesObservationsAndSuppressesPublication(t *testing.T) {
	initSettingTestDB(t)
	ipmonitor.ResetCaches()
	t.Cleanup(ipmonitor.ResetCaches)
	user := model.Client{Name: "alice", Enable: true, Up: 4, Down: 5}
	if err := dbsqlite.DB().Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	ipmonitor.Record(user.Name, "198.51.100.1")
	db := dbsqlite.DB()
	for _, query := range []string{
		"CREATE TABLE accounting_parent (id INTEGER PRIMARY KEY)",
		"CREATE TABLE accounting_child (parent_id INTEGER, FOREIGN KEY(parent_id) REFERENCES accounting_parent(id) DEFERRABLE INITIALLY DEFERRED)",
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	injected := false
	callback := "test-accounting-deferred-commit-failure"
	if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
		if injected || tx.Statement == nil || tx.Statement.Table != "stats" {
			return
		}
		injected = true
		if err := tx.Exec("INSERT INTO accounting_child(parent_id) VALUES (1)").Error; err != nil {
			_ = tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	published, failed := 0, 0
	owner := Recorder{Publish: func(OnlineResources, []model.Stats) { published++ }, CommitFailed: func() { failed++ }}
	err := owner.PersistSamples(true, []Sample{{VPNUserID: user.VPNUserID(), Resource: "user", Tag: user.Name, Direction: true, Traffic: 100}})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "foreign key") || !injected {
		t.Fatalf("expected actual deferred commit failure: %v", err)
	}
	var actual model.Client
	if err := db.First(&actual, user.Id).Error; err != nil {
		t.Fatal(err)
	}
	var rows int64
	if err := db.Model(&model.Stats{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if published != 0 || failed != 1 || rows != 0 || actual.Up != user.Up || actual.Down != user.Down {
		t.Fatal("failed commit published or persisted accounting effects")
	}
	if err := ipmonitor.Flush(); err != nil {
		t.Fatal(err)
	}
	history, err := ipmonitor.History(user.Name, 10)
	if err != nil || len(history) != 1 {
		t.Fatalf("failed accounting commit lost its IP batch: rows=%d err=%v", len(history), err)
	}
}
