package backup

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestPreRestoreRecoveryPublicationFaultMatrixLeavesNoAdvertisedFile(t *testing.T) {
	restorer := recoveryPublicationFixture(t)
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
			if _, err := restorer.preservePreRestoreBackupAt(context.Background(), root, ops); err == nil {
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

func recoveryPublicationFixture(t *testing.T) Restorer {
	t.Helper()
	source := filepath.Join(t.TempDir(), "snapshot.db")
	if err := os.WriteFile(source, []byte("SQLite format 3\x00canonical snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	return Restorer{PrepareExport: func(context.Context, string) (string, func(), error) { return source, func() {}, nil }}
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
