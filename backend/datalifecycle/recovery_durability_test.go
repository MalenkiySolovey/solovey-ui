//go:build !minimal

package datalifecycle

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRecoveryPublicationFaultsNeverLeaveAdvertisedOrInvisibleState(t *testing.T) {
	fault := errors.New("injected recovery publication fault")
	cases := []string{"directory-create", "temporary-create", "temporary-sync", "temporary-close", "rename", "directory-sync", "reopen"}
	for _, stage := range cases {
		t.Run(stage, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "recovery", "drop-data")
			if stage != "directory-create" {
				if err := os.MkdirAll(root, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			destination := filepath.Join(root, portableDropRecoveryFilename("data-operation:"+strings.Repeat("a", 48)))
			ops := productionRecoveryFileOps
			switch stage {
			case "directory-create":
				ops.mkdirAll = func(string, fs.FileMode) error { return fault }
			case "temporary-create":
				ops.openFile = func(string, int, fs.FileMode) (recoveryWriteFile, error) { return nil, fault }
			case "temporary-sync", "temporary-close":
				ops.openFile = func(path string, flag int, mode fs.FileMode) (recoveryWriteFile, error) {
					file, err := os.OpenFile(path, flag, mode)
					return &faultRecoveryWriteFile{File: file, faultSync: stage == "temporary-sync", faultClose: stage == "temporary-close", fault: fault}, err
				}
			case "rename":
				ops.rename = func(string, string) error { return fault }
			case "directory-sync":
				ops.syncDirectory = func(string) error { return fault }
			case "reopen":
				ops.open = func(string) (recoveryReadFile, error) { return nil, fault }
			}
			if _, err := publishRecoveryFile(context.Background(), bytes.NewReader([]byte("recovery-content")), destination, 1024, ops); err == nil {
				t.Fatal("faulted publication succeeded")
			}
			if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("fault left published destination: %v", err)
			}
		})
	}
}

func TestBackupReferenceCommitFailureReclaimsPublishedFile(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "commit-fault.db")), &gorm.Config{})
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
	now := time.Unix(2_000_000_000, 0).UTC()
	root := filepath.Join(t.TempDir(), "drop")
	manager := &Manager{DB: func() *gorm.DB { return db }, Now: func() time.Time { return now }, Root: root, RestoreRoot: filepath.Join(t.TempDir(), "restore")}
	operation := model.DataLifecycleOperation{OperationID: "data-operation:" + strings.Repeat("c", 48), IdempotencyKey: "commit-fault-recovery",
		Kind: "DROP_DATA", State: "BACKING_UP", OwnerID: "fixture-owner", ManifestDigest: dataLifecycleTestDigest("manifest"),
		ExpectedRevision: dataLifecycleTestDigest("expected"), Revision: 2, CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
	if err := db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, portableDropRecoveryFilename(operation.OperationID))
	backupRef, err := publishRecoveryFile(context.Background(), bytes.NewReader([]byte("commit-boundary")), destination, 1024, manager.filesystem())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_data_backup_ref BEFORE UPDATE OF backup_ref ON data_lifecycle_operations_v1
WHEN NEW.backup_ref != '' BEGIN SELECT RAISE(FAIL, 'injected ref commit failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := manager.commitBackupReady(context.Background(), operation, backupRef); err == nil {
		t.Fatal("SQLite reference commit fault was accepted")
	}
	var stored model.DataLifecycleOperation
	if err := db.First(&stored, "operation_id = ?", operation.OperationID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.BackupRef != "" || stored.State != "BACKING_UP" {
		t.Fatalf("failed commit mutated authority: %#v", stored)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed commit left orphan=%v", err)
	}
}

type faultRecoveryWriteFile struct {
	*os.File
	faultSync  bool
	faultClose bool
	fault      error
}

func (f *faultRecoveryWriteFile) Sync() error {
	if f.faultSync {
		return f.fault
	}
	return f.File.Sync()
}

func (f *faultRecoveryWriteFile) Close() error {
	err := f.File.Close()
	if f.faultClose {
		return errors.Join(err, f.fault)
	}
	return err
}

var _ io.WriteCloser = (*faultRecoveryWriteFile)(nil)
