package deployment

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	sqlite3 "github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

// A real independent panel writer commits just before the posture write. On
// beta.6 the deferred transaction has already read its now-stale WAL snapshot.
func TestDoctorPersistenceWALConcurrentWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.db")
	if err := dbsqlite.Init(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	db := dbsqlite.DB()
	for _, row := range []any{&model.DeploymentState{}, &model.DeploymentOperation{}, &model.DeploymentJournal{}, &model.DeploymentDoctorSnapshot{}} {
		var count int64
		if err := db.Model(row).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("initial deployment rows=%d err=%v", count, err)
		}
	}
	writer, err := sql.Open("sqlite3", path+"?_busy_timeout=10000&_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	var mode string
	var timeout int
	if err := db.Raw("PRAGMA journal_mode").Scan(&mode).Error; err != nil || mode != "wal" {
		t.Fatalf("journal=%s err=%v", mode, err)
	}
	if err := db.Raw("PRAGMA busy_timeout").Scan(&timeout).Error; err != nil || timeout != 10000 {
		t.Fatalf("timeout=%d err=%v", timeout, err)
	}
	now := time.Now().UTC()
	manager := NewManager(NewRepository(db), &fakeProvider{current: domain.NativeHardened, now: now})
	manager.Now = func() time.Time { return now }
	ctx := context.Background()
	if _, err := manager.Status(ctx); err != nil {
		t.Fatal(err)
	}
	armed := true
	writes := 0
	var storageErr error
	before := func(tx *gorm.DB) {
		if !armed || tx.Statement.Table != "deployment_state_v1" {
			return
		}
		armed = false
		_, err := writer.Exec("INSERT INTO settings(key,value) VALUES(?,?)", "deployment-wal-regression", "bounded-test-value")
		if err != nil {
			tx.AddError(err)
			return
		}
		writes++
	}
	after := func(tx *gorm.DB) {
		if tx.Statement.Table == "deployment_state_v1" && tx.Error != nil {
			storageErr = tx.Error
		}
	}
	if err := db.Callback().Update().Before("gorm:update").Register("wal_writer", before); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("wal_writer", before); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().After("gorm:update").Register("wal_capture", after); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().After("gorm:create").Register("wal_capture", after); err != nil {
		t.Fatal(err)
	}
	report, err := manager.Doctor(ctx)
	if err != nil {
		var code sqlite3.Error
		if errors.As(storageErr, &code) {
			t.Logf("PERSISTENCE_FAILURE_STAGE=SAVE_POSTURE SQLITE_CODE=%d SQLITE_EXTENDED_CODE=%d", code.Code, code.ExtendedCode)
		}
		t.Fatalf("Status -> Doctor with independent WAL writer: %v", err)
	}
	if writes != 1 {
		t.Fatalf("concurrent writer commits=%d", writes)
	}
	state, err := manager.Repository.State(ctx)
	if err != nil || state.DoctorRevision != report.Revision {
		t.Fatalf("doctor revision coherence failed: %v", err)
	}
	var snapshot model.DeploymentDoctorSnapshot
	if err := db.Where("revision = ?", state.DoctorRevision).Take(&snapshot).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Doctor(ctx); err != nil {
		t.Fatal(err)
	}
}
