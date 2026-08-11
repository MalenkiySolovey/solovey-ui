package datalifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWriteRestoredOperationReplacesItsDistrustedSnapshotIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:restore-operation-replace?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DataLifecycleOperation{}, &model.DataLifecycleJournal{}); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{DB: func() *gorm.DB { return db }}
	operation := model.DataLifecycleOperation{OperationID: "data-operation:restore-test", IdempotencyKey: "restore-idempotency",
		Kind: "RESTORE", State: "APPLIED", OwnerID: "core", ManifestDigest: restoreTestDigest("manifest"),
		BackupRef: restoreTestDigest("backup"), ExpectedRevision: restoreTestDigest("rehearsal"), Revision: 3, CreatedAt: 10, UpdatedAt: 20}
	distrusted := operation
	distrusted.State, distrusted.Revision, distrusted.RestoredUntrusted = "RECOVERY_REQUIRED", 1, true
	if err := db.Create(&distrusted).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.DataLifecycleJournal{OperationID: operation.OperationID, State: distrusted.State,
		Event: "restored_untrusted", Revision: 1, CreatedAt: 11}).Error; err != nil {
		t.Fatal(err)
	}
	if err := manager.writeRestoredOperation(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	var stored model.DataLifecycleOperation
	if err := db.First(&stored, "operation_id = ?", operation.OperationID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.State != "APPLIED" || stored.Revision != operation.Revision || stored.RestoredUntrusted {
		t.Fatalf("restored operation authority=%#v", stored)
	}
	var journals []model.DataLifecycleJournal
	if err := db.Where("operation_id = ?", operation.OperationID).Find(&journals).Error; err != nil {
		t.Fatal(err)
	}
	if len(journals) != 1 || journals[0].Event != "restore_applied" || journals[0].Revision != operation.Revision {
		t.Fatalf("restored operation journals=%#v", journals)
	}
}

func TestWriteRestoredOperationRejectsSplitIdentityCollision(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:restore-operation-ambiguous?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DataLifecycleOperation{}, &model.DataLifecycleJournal{}); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{DB: func() *gorm.DB { return db }}
	wanted := model.DataLifecycleOperation{OperationID: "data-operation:wanted", IdempotencyKey: "wanted-key", Kind: "RESTORE",
		State: "APPLIED", OwnerID: "core", ManifestDigest: restoreTestDigest("manifest"), ExpectedRevision: restoreTestDigest("rehearsal"), Revision: 3}
	first, second := wanted, wanted
	first.IdempotencyKey = "other-key"
	second.OperationID = "data-operation:other"
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	if err := manager.writeRestoredOperation(context.Background(), wanted); err == nil {
		t.Fatal("split restored operation identity was accepted")
	}
	var count int64
	if err := db.Model(&model.DataLifecycleOperation{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("ambiguous failure mutated rows=%d err=%v", count, err)
	}
}

func TestOperationProjectsMalformedPersistedAuthorityAsRecoveryRequired(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:restore-operation-malformed?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DataLifecycleOperation{}, &model.DataLifecycleJournal{}); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{DB: func() *gorm.DB { return db }}
	row := model.DataLifecycleOperation{
		OperationID: "data-operation:malformed", IdempotencyKey: "malformed-key",
		Kind: "RESTORE", State: "APPLIED", OwnerID: "core",
		ExpectedRevision: restoreTestDigest("rehearsal"), Revision: 3,
		CreatedAt: 10, UpdatedAt: 20,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	projected, err := manager.Operation(context.Background(), row.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if projected.State != "RECOVERY_REQUIRED" || projected.ReasonCode != "data_lifecycle_operation_state_invalid" ||
		!projected.RestoredUntrusted {
		t.Fatalf("malformed operation was trusted: %#v", projected)
	}
}

func restoreTestDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
