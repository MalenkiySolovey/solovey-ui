package datalifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDataLifecycleRetentionPreservesLiveClosureAndBoundsTerminalRowsFilesAndOrphans(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "retention.db")), &gorm.Config{})
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
	now := time.Unix(2_100_000_000, 0).UTC()
	root, restoreRoot := filepath.Join(t.TempDir(), "drop"), filepath.Join(t.TempDir(), "restore")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(restoreRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{DB: func() *gorm.DB { return db }, Now: func() time.Time { return now }, Root: root, RestoreRoot: restoreRoot}
	content := []byte("duplicate-recovery-digest")
	backupRef := retentionTestDigest(string(content))
	rows := make([]model.DataLifecycleOperation, 0, 154)
	for index := 0; index < 12; index++ {
		operation := retentionOperation(now.Add(-time.Duration(index)*time.Minute), "applied-"+twoDigits(index), "APPLIED")
		operation.BackupRef = backupRef
		rows = append(rows, operation)
		if err := os.WriteFile(filepath.Join(root, portableDropRecoveryFilename(operation.OperationID)), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < 140; index++ {
		rows = append(rows, retentionOperation(now.Add(-time.Duration(1000+index)*time.Minute), "failed-"+threeDigits(index), "FAILED"))
	}
	live := retentionOperation(now.Add(-time.Hour), "live", "RECOVERY_REQUIRED")
	live.BackupRef = backupRef
	rows = append(rows, live)
	if err := os.WriteFile(filepath.Join(root, portableDropRecoveryFilename(live.OperationID)), content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, operation := range rows {
		if err := db.Create(&model.DataLifecycleJournal{OperationID: operation.OperationID, State: operation.State,
			Event: "retention-fixture", Revision: operation.Revision, CreatedAt: operation.UpdatedAt}).Error; err != nil {
			t.Fatal(err)
		}
	}
	ownedPartial := filepath.Join(root, portableDropRecoveryFilename("data-operation:"+strings.Repeat("f", 48))+".partial")
	foreign := filepath.Join(root, "operator-backup.db")
	if err := os.WriteFile(ownedPartial, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Prune(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedOperations == 0 || result.DeletedArtifacts < 5 {
		t.Fatalf("prune result=%#v", result)
	}
	var terminalCount, liveCount, orphanJournals int64
	if err := db.Model(&model.DataLifecycleOperation{}).Where("state IN ?", []string{"APPLIED", "FAILED", "ROLLED_BACK"}).Count(&terminalCount).Error; err != nil {
		t.Fatal(err)
	}
	if terminalCount > maxDataLifecycleTerminalOperations {
		t.Fatalf("terminal rows=%d", terminalCount)
	}
	if err := db.Model(&model.DataLifecycleOperation{}).Where("operation_id = ?", live.OperationID).Count(&liveCount).Error; err != nil || liveCount != 1 {
		t.Fatalf("live closure count=%d err=%v", liveCount, err)
	}
	if err := db.Model(&model.DataLifecycleJournal{}).Where("operation_id NOT IN (?)", db.Model(&model.DataLifecycleOperation{}).Select("operation_id")).Count(&orphanJournals).Error; err != nil || orphanJournals != 0 {
		t.Fatalf("orphan journals=%d err=%v", orphanJournals, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	ownedDatabases := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "drop-") && strings.HasSuffix(entry.Name(), ".db") {
			ownedDatabases++
		}
	}
	if ownedDatabases > maxDataLifecycleTerminalArtifacts+1 {
		t.Fatalf("owned recovery files=%d entries=%v", ownedDatabases, entries)
	}
	if _, err := os.Stat(filepath.Join(root, portableDropRecoveryFilename(live.OperationID))); err != nil {
		t.Fatalf("live artifact removed: %v", err)
	}
	if _, err := os.Stat(ownedPartial); !os.IsNotExist(err) {
		t.Fatalf("owned partial survived: %v", err)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "foreign" {
		t.Fatalf("foreign file changed data=%q err=%v", data, err)
	}
	second, err := manager.Prune(context.Background())
	if err != nil || second.DeletedOperations != 0 || second.DeletedArtifacts != 0 {
		t.Fatalf("restart/idempotent prune=%#v err=%v", second, err)
	}
}

func TestRestoreDuplicateDigestSharesOneRetainedRecoveryArtifact(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "restore-digest.db")), &gorm.Config{})
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
	now := time.Unix(2_200_000_000, 0).UTC()
	restoreRoot := filepath.Join(t.TempDir(), "restore")
	if err := os.MkdirAll(restoreRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("shared-restore-recovery")
	ref := retentionTestDigest(string(content))
	if err := os.WriteFile(filepath.Join(restoreRoot, "pre-restore-"+ref+".db"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{DB: func() *gorm.DB { return db }, Now: func() time.Time { return now }, Root: filepath.Join(t.TempDir(), "drop"), RestoreRoot: restoreRoot}
	for index := 0; index < 2; index++ {
		operation := retentionOperation(now.Add(-time.Duration(index)*time.Minute), "restore-"+twoDigits(index), "APPLIED")
		operation.Kind, operation.OwnerID, operation.BackupRef = "RESTORE", "core", ref
		if err := db.Create(&operation).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := manager.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.DataLifecycleOperation{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("duplicate digest operations=%d err=%v", count, err)
	}
	if _, err := os.Stat(filepath.Join(restoreRoot, "pre-restore-"+ref+".db")); err != nil {
		t.Fatal(err)
	}
}

func retentionOperation(now time.Time, suffix, state string) model.DataLifecycleOperation {
	return model.DataLifecycleOperation{OperationID: "data-operation:" + suffix, IdempotencyKey: "idem-" + suffix,
		Kind: "DROP_DATA", State: state, OwnerID: "fixture-owner", ManifestDigest: retentionTestDigest("manifest"),
		ExpectedRevision: retentionTestDigest("expected"), Revision: 3, CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
}

func retentionTestDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func twoDigits(value int) string { return string(rune('0'+value/10)) + string(rune('0'+value%10)) }

func threeDigits(value int) string {
	return string(rune('0'+value/100)) + string(rune('0'+(value/10)%10)) + string(rune('0'+value%10))
}
