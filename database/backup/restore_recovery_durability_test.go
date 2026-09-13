package backup

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestPreRestoreRecoveryPublicationIsDurableAndReopenableAcrossSuccessiveSnapshots(t *testing.T) {
	initRestoreRecoveryPublicationDB(t)
	root := filepath.Join(t.TempDir(), "recovery", "restore")
	digest, err := preservePreRestoreBackupAt(context.Background(), root, productionRestoreRecoveryFileOps)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "pre-restore-"+digest+".db")
	if bytes, err := verifyRestoreRecoveryFile(context.Background(), path, digest, productionRestoreRecoveryFileOps); err != nil || bytes <= 0 {
		t.Fatalf("strict recovery reopen bytes=%d err=%v", bytes, err)
	}
	again, err := preservePreRestoreBackupAt(context.Background(), root, productionRestoreRecoveryFileOps)
	if err != nil {
		t.Fatal(err)
	}
	againPath := filepath.Join(root, "pre-restore-"+again+".db")
	if bytes, err := verifyRestoreRecoveryFile(context.Background(), againPath, again, productionRestoreRecoveryFileOps); err != nil || bytes <= 0 {
		t.Fatalf("successive strict recovery reopen bytes=%d err=%v", bytes, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) < 1 || len(entries) > 2 {
		t.Fatalf("successive publication entries=%v err=%v", entries, err)
	}
}

func TestPreRestoreRecoveryPublicationFaultMatrixLeavesNoAdvertisedFile(t *testing.T) {
	initRestoreRecoveryPublicationDB(t)
	fault := errors.New("injected pre-restore publication fault")
	for _, stage := range []string{"directory-create", "temporary-create", "temporary-sync", "temporary-close", "rename", "directory-sync", "reopen"} {
		t.Run(stage, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "recovery", "restore")
			if stage != "directory-create" {
				if err := os.MkdirAll(root, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			ops := productionRestoreRecoveryFileOps
			switch stage {
			case "directory-create":
				ops.mkdirAll = func(string, fs.FileMode) error { return fault }
			case "temporary-create":
				ops.createTemp = func(string, string) (restoreRecoveryTempFile, error) { return nil, fault }
			case "temporary-sync", "temporary-close":
				ops.createTemp = func(directory, pattern string) (restoreRecoveryTempFile, error) {
					file, err := os.CreateTemp(directory, pattern)
					return &faultRestoreRecoveryTemp{File: file, faultSync: stage == "temporary-sync", faultClose: stage == "temporary-close", fault: fault}, err
				}
			case "rename":
				ops.rename = func(string, string) error { return fault }
			case "directory-sync":
				ops.syncDirectory = func(string) error { return fault }
			case "reopen":
				ops.open = func(string) (restoreRecoveryReadFile, error) { return nil, fault }
			}
			if _, err := preservePreRestoreBackupAt(context.Background(), root, ops); err == nil {
				t.Fatal("faulted pre-restore publication succeeded")
			}
			entries, readErr := os.ReadDir(root)
			if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
				t.Fatal(readErr)
			}
			for _, entry := range entries {
				if owned := entry.Name(); len(owned) >= len("pre-restore-") && owned[:len("pre-restore-")] == "pre-restore-" {
					t.Fatalf("fault left owned recovery file %q", owned)
				}
			}
		})
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

type faultRestoreRecoveryTemp struct {
	*os.File
	faultSync  bool
	faultClose bool
	fault      error
}

func (f *faultRestoreRecoveryTemp) Sync() error {
	if f.faultSync {
		return f.fault
	}
	return f.File.Sync()
}

func (f *faultRestoreRecoveryTemp) Close() error {
	err := f.File.Close()
	if f.faultClose {
		return errors.Join(err, f.fault)
	}
	return err
}
