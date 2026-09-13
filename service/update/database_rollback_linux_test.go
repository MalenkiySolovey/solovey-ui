//go:build linux

package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	backupdb "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

func TestRollbackExactDatabaseRollbackRestoresLogicalGenerationBeforeRebind(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	backupdb.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() {
		backupdb.SetSendSighupHook(nil)
		_ = dbsqlite.Close()
	})
	const markerKey = "rollback-logical-generation"
	if err := dbsqlite.DB().Create(&model.Setting{Key: markerKey, Value: "A"}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Unix()
	operation := model.UpdateOperation{OperationID: "update-operation:rollback-e2e", IdempotencyKey: "idempotency:rollback-e2e", State: string(StatePreflighting),
		Channel: string(release.ChannelMain), Sequence: 2, ReleaseID: "solovey-ui-main-rollback", Version: "2026.4.0",
		ManifestDigest: rollbackDigest("release"), ArtifactSetDigest: rollbackDigest("artifacts"), Platform: "linux", Arch: "amd64", BinaryProfile: "full",
		DeploymentRevision: rollbackDigest("deployment"), BrokerCapability: "broker-capabilities-1.3", MigrationSetDigest: rollbackDigest("migration"),
		RestartClass: "stack", RebootClass: "operator-advisory", RollbackClass: "automatic", BytesTotal: 1, Revision: 5, CreatedAt: now, UpdatedAt: now}
	if err := dbsqlite.DB().Create(&operation).Error; err != nil {
		t.Fatal(err)
	}

	backupPath, cleanup, err := backupdb.PrepareExportContext(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	input, err := os.Open(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	rehearsal, err := backupdb.Rehearse(context.Background(), input)
	_ = input.Close()
	if err != nil || !rehearsal.Possible {
		t.Fatalf("rehearsal=%#v err=%v", rehearsal, err)
	}
	provider := &BrokerProvider{Root: filepath.Join(dir, "update-cache"), backupOps: productionUpdateBackupOps}
	operation.BackupRef = rehearsal.BackupDigest
	if _, err := provider.publishDatabaseBackup(context.Background(), operation, backupPath, rehearsal); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", markerKey).Update("value", "B").Error; err != nil {
		t.Fatal(err)
	}

	rollbackPath := filepath.Join(provider.Root, operation.OperationID, "database-rollback.db")
	rollbackFile, err := os.Open(rollbackPath)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := backupdb.RestoreContextDetailed(context.Background(), rollbackFile)
	_ = rollbackFile.Close()
	if err != nil || !restored.Rehearsal.Possible || restored.Rehearsal.BackupDigest != operation.BackupRef {
		t.Fatalf("restore result=%#v err=%v", restored, err)
	}
	provider.restorePending = true
	repository := Repository{DB: dbsqlite.DB}
	operation, err = repository.rebindRestoredRollback(context.Background(), operation, "rollback_exact_database_rollback")
	if err != nil {
		t.Fatal(err)
	}
	if State(operation.State) != StateRolledBack || operation.RestoredUntrusted || operation.RollbackAvailable || operation.BackupRef != "" {
		t.Fatalf("rollback rebind=%#v", operation)
	}
	if err := provider.CompleteDatabaseRollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	var setting model.Setting
	if err := dbsqlite.DB().Where("key = ?", markerKey).First(&setting).Error; err != nil || setting.Value != "A" {
		t.Fatalf("logical database generation=%#v err=%v", setting, err)
	}
	var stored model.UpdateOperation
	if err := dbsqlite.DB().Where("operation_id = ?", operation.OperationID).First(&stored).Error; err != nil || State(stored.State) != StateRolledBack || stored.RestoredUntrusted {
		t.Fatalf("stored rollback=%#v err=%v", stored, err)
	}
}

func rollbackDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
