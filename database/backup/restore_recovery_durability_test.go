package backup

import (
	"context"
	canonical "github.com/MalenkiySolovey/solovey-backend/storage/backup"
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestPreRestoreRecoveryPublicationIsDurableAndReopenableAcrossSuccessiveSnapshots(t *testing.T) {
	initRestoreRecoveryPublicationDB(t)
	root := filepath.Join(t.TempDir(), "recovery", "restore")
	digest, err := backupRestorer().PreserveRecovery(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "pre-restore-"+digest+".db")
	if bytes, err := canonical.VerifyRecovery(context.Background(), path, digest); err != nil || bytes <= 0 {
		t.Fatalf("strict recovery reopen bytes=%d err=%v", bytes, err)
	}
	again, err := backupRestorer().PreserveRecovery(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	againPath := filepath.Join(root, "pre-restore-"+again+".db")
	if bytes, err := canonical.VerifyRecovery(context.Background(), againPath, again); err != nil || bytes <= 0 {
		t.Fatalf("successive strict recovery reopen bytes=%d err=%v", bytes, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) < 1 || len(entries) > 2 {
		t.Fatalf("successive publication entries=%v err=%v", entries, err)
	}
}

func initRestoreRecoveryPublicationDB(t *testing.T) {
	t.Helper()
	if dbsqlite.DB() != nil {
		_ = dbsqlite.Close()
	}
	databaseRoot := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", databaseRoot)
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&model.Setting{Key: "recovery-restore-recovery", Value: "published"}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
}
