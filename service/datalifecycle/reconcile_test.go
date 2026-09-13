package datalifecycle

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestStartupReconciliationClosesSafeInterruptionsAndFencesAmbiguousOnes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "reconcile.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.DataLifecycleOperation{}, &model.DataLifecycleJournal{}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_900_000_000, 0)
	manager := NewManager()
	manager.DB, manager.Now = func() *gorm.DB { return db }, func() time.Time { return now }
	manager.Root, manager.RestoreRoot = filepath.Join(t.TempDir(), "drop"), filepath.Join(t.TempDir(), "restore")
	if err := os.MkdirAll(manager.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(manager.RestoreRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	operations := []model.DataLifecycleOperation{
		reconcileFixture("restore", "RESTORE", "RESTORING", false),
		reconcileFixture("drop-before", "DROP_DATA", "BACKUP_READY", false),
		reconcileFixture("drop-after", "DROP_DATA", "DROPPING", false),
		reconcileFixture("restored", "RESTORE", "APPLIED", true),
	}
	if err := db.Create(&operations).Error; err != nil {
		t.Fatal(err)
	}
	for _, operation := range operations {
		if operation.BackupRef == "" {
			continue
		}
		path := filepath.Join(manager.RestoreRoot, "pre-restore-"+operation.BackupRef+".db")
		if operation.Kind == "DROP_DATA" {
			path = filepath.Join(manager.Root, portableDropRecoveryFilename(operation.OperationID))
		}
		if err := os.WriteFile(path, []byte("backup"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	wanted := map[string]string{
		"data-operation:restore":     "ROLLED_BACK",
		"data-operation:drop-before": "ROLLED_BACK",
		"data-operation:drop-after":  "RECOVERY_REQUIRED",
		"data-operation:restored":    "RECOVERY_REQUIRED",
	}
	for id, state := range wanted {
		var observed model.DataLifecycleOperation
		if err := db.First(&observed, "operation_id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		if observed.State != state || observed.Revision != 2 {
			t.Fatalf("%s state=%s revision=%d want=%s/2", id, observed.State, observed.Revision, state)
		}
	}
	var journals int64
	if err := db.Model(&model.DataLifecycleJournal{}).Count(&journals).Error; err != nil || journals != int64(len(wanted)) {
		t.Fatalf("startup reconciliation journals=%d err=%v", journals, err)
	}
}

func TestStartupReconciliationFailsClosedWhenCommittedRecoveryReferenceCannotReopen(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "missing-reference.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err != nil {
		t.Fatal(err)
	} else {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := db.AutoMigrate(&model.DataLifecycleOperation{}, &model.DataLifecycleJournal{}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_900_100_000, 0).UTC()
	manager := &Manager{DB: func() *gorm.DB { return db }, Now: func() time.Time { return now },
		Root: filepath.Join(t.TempDir(), "drop"), RestoreRoot: filepath.Join(t.TempDir(), "restore")}
	operation := reconcileFixture("missing-artifact", "DROP_DATA", "APPLIED", false)
	operation.CreatedAt, operation.UpdatedAt = now.Unix(), now.Unix()
	if err := db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	if err := manager.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	var observed model.DataLifecycleOperation
	if err := db.First(&observed, "operation_id = ?", operation.OperationID).Error; err != nil {
		t.Fatal(err)
	}
	if observed.State != "RECOVERY_REQUIRED" || observed.ReasonCode != "data_lifecycle_recovery_backup_unavailable" || observed.Revision != operation.Revision+1 {
		t.Fatalf("missing committed reference remained trusted: %#v", observed)
	}
}

func reconcileFixture(suffix, kind, state string, restored bool) model.DataLifecycleOperation {
	operation := model.DataLifecycleOperation{
		OperationID: "data-operation:" + suffix, IdempotencyKey: "key-" + suffix, Kind: kind, State: state,
		OwnerID: "core", ManifestDigest: restoreTestDigest("manifest"), ExpectedRevision: restoreTestDigest("revision"), Revision: 1,
		RestoredUntrusted: restored, CreatedAt: 1_800_000_000, UpdatedAt: 1_800_000_000,
	}
	if kind == "DROP_DATA" {
		operation.OwnerID = "fixture-owner"
	}
	if state == "BACKUP_READY" || state == "DROPPING" || state == "VERIFYING" || state == "APPLIED" {
		operation.BackupRef = restoreTestDigest("backup")
	}
	return operation
}
