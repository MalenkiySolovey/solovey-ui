package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	contract "github.com/MalenkiySolovey/solovey-ui/internal/ops/updatebroker"
)

func TestRollbackDatabaseRollbackPublicationFaultMatrixReopensExactBackup(t *testing.T) {
	data := []byte("logical-snapshot-rollback")
	sum := sha256.Sum256(data)
	rehearsal := backup.RestoreRehearsal{Possible: true, BackupDigest: hex.EncodeToString(sum[:]), BackupBytes: int64(len(data))}
	fault := errors.New("rollback publication fault")
	cases := []struct {
		name  string
		fault string
	}{
		{"first-directory-sync", "first-dir"},
		{"operation-directory-sync", "operation-dir"},
		{"create", "create"},
		{"write", "write"},
		{"file-sync", "sync"},
		{"close", "close"},
		{"rename", "rename"},
		{"final-directory-sync", "final-dir"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			base := t.TempDir()
			source := filepath.Join(base, "source.db")
			if err := os.WriteFile(source, data, 0o600); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(base, "update-cache")
			provider := &BrokerProvider{Root: root, backupOps: rollbackFaultBackupOps(test.fault, fault)}
			operation := model.UpdateOperation{OperationID: "update-operation-rollback-fault-" + test.fault}
			got, err := provider.publishDatabaseBackup(context.Background(), operation, source, rehearsal)
			if !errors.Is(err, fault) {
				t.Fatalf("publication err=%v, want %v", err, fault)
			}
			destination := filepath.Join(root, operation.OperationID, "database-rollback.db")
			if test.fault != "final-dir" {
				if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("failed publication left destination stat=%v", statErr)
				}
			}
			if got != "" {
				t.Fatalf("failed publication returned backup ref %q", got)
			}
			provider.backupOps = productionUpdateBackupOps
			got, err = provider.publishDatabaseBackup(context.Background(), operation, source, rehearsal)
			if err != nil || got != rehearsal.BackupDigest {
				t.Fatalf("retry digest=%q err=%v", got, err)
			}
			if reopened, err := updateBackupFileDigest(productionUpdateBackupOps, destination, backup.MaxRestoreBytes); err != nil || reopened != rehearsal.BackupDigest {
				t.Fatalf("reopened backup=%q err=%v", reopened, err)
			}
		})
	}
}

func TestRollbackLiveRollbackKeepSetRetainsOnlyObservedCurrentAuthority(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("operation-bound cache names use the Linux broker operation-id namespace")
	}
	base := filepath.Join(t.TempDir(), "update-cache")
	provider := &BrokerProvider{Root: base, observeRollbackAuthority: func(context.Context, model.UpdateOperation) (contract.ObservationV1, error) {
		return contract.ObservationV1{ProviderRevision: contract.ProviderRevision, ManagementReady: true,
			RollbackAvailable: true, RollbackOperationID: "update-operation:applied-a", RollbackManifestDigest: updateDigestForRollback("a"),
			RollbackTargetSequence: 4, RollbackTargetDigest: updateDigestForRollback("predecessor"),
			RollbackRef: broker.Digest([]byte("rollback:update-operation:applied-a:" + updateDigestForRollback("a")))}, nil
	}}
	if err := os.MkdirAll(filepath.Join(base, "update-operation:applied-a"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"update-operation:failed-b", "update-operation:rolled-back-c"} {
		if err := os.MkdirAll(filepath.Join(base, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := provider.CleanupTerminal(context.Background(), model.UpdateOperation{OperationID: "update-operation:failed-b", State: string(StateFailed)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "update-operation:applied-a")); err != nil {
		t.Fatalf("live A backup was removed: %v", err)
	}
	for _, name := range []string{"update-operation:failed-b", "update-operation:rolled-back-c"} {
		if _, err := os.Stat(filepath.Join(base, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("obsolete cache %s remains: %v", name, err)
		}
	}
}

type rollbackBackupFaultFile struct {
	*os.File
	faultAt string
	fault   error
}

func (file *rollbackBackupFaultFile) Write(data []byte) (int, error) {
	if file.faultAt == "write" {
		return 0, file.fault
	}
	return file.File.Write(data)
}

func (file *rollbackBackupFaultFile) ReadFrom(reader io.Reader) (int64, error) {
	if file.faultAt == "write" {
		return 0, file.fault
	}
	return io.Copy(file.File, reader)
}

func (file *rollbackBackupFaultFile) Sync() error {
	if file.faultAt == "sync" {
		return file.fault
	}
	return file.File.Sync()
}

func (file *rollbackBackupFaultFile) Close() error {
	if file.faultAt == "close" {
		_ = file.File.Close()
		return file.fault
	}
	return file.File.Close()
}

func rollbackFaultBackupOps(faultAt string, fault error) updateBackupOps {
	ops := productionUpdateBackupOps
	var syncCount int
	ops.syncDirectory = func(path string) error {
		syncCount++
		if faultAt == "first-dir" && syncCount == 1 || faultAt == "operation-dir" && syncCount == 2 || faultAt == "final-dir" && syncCount == 3 {
			return fault
		}
		return syncUpdateBackupDirectory(path)
	}
	ops.createTemp = func(directory, pattern string) (updateBackupFile, error) {
		if faultAt == "create" {
			return nil, fault
		}
		file, err := os.CreateTemp(directory, pattern)
		if err != nil {
			return nil, err
		}
		return &rollbackBackupFaultFile{File: file, faultAt: faultAt, fault: fault}, nil
	}
	if faultAt == "rename" {
		ops.rename = func(string, string) error { return fault }
	}
	return ops
}

func updateDigestForRollback(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
