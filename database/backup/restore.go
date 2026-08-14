package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/restorestate"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

// Restore validates and atomically installs an uploaded Solovey UI database.
func Restore(file multipart.File) error {
	return RestoreContext(context.Background(), file)
}

type RestoreExecutionResult struct {
	Rehearsal              RestoreRehearsal `json:"rehearsal"`
	RecoveryBackupRef      string           `json:"recoveryBackupRef"`
	RecoveryCleanupPending bool             `json:"recoveryCleanupPending"`
	RestartPending         bool             `json:"restartPending"`
}

func RestoreContext(ctx context.Context, file multipart.File) error {
	_, err := RestoreContextDetailed(ctx, file)
	if err != nil {
		return err
	}
	_, _, err = CompletePendingRestore(ctx)
	return err
}

func RestoreContextDetailed(ctx context.Context, file io.ReadSeeker) (RestoreExecutionResult, error) {
	result := RestoreExecutionResult{}
	if ctx == nil {
		return result, common.NewError("Restore context is required")
	}
	rehearsal, rehearsalErr := Rehearse(ctx, file)
	result.Rehearsal = rehearsal
	if rehearsalErr != nil {
		return result, common.NewErrorf("Restore rehearsal rejected the backup: %v", rehearsalErr)
	}
	if !rehearsal.Possible {
		return result, common.NewErrorf("Restore rehearsal rejected the backup: %s", strings.Join(rehearsal.ReasonCodes, ","))
	}
	valid, err := IsSQLite(file)
	if err != nil {
		return result, common.NewErrorf("Error checking db file format: %v", err)
	}
	if !valid {
		return result, common.NewError("Invalid db file format")
	}
	if _, err = file.Seek(0, 0); err != nil {
		return result, common.NewErrorf("Error resetting file reader: %v", err)
	}

	dbPath := configstorage.GetDBPath()
	tempPath := restorestate.StagingPath(dbPath)
	fallbackPath := restorestate.FallbackPath(dbPath)
	if err := restorestate.EnsureIdle(dbPath); err != nil {
		return result, common.NewErrorf("Database restore recovery is required: %v", err)
	}
	if result.RecoveryBackupRef, err = preservePreRestoreBackup(ctx); err != nil {
		return result, common.NewErrorf("Error preserving pre-restore recovery backup: %v", err)
	}
	if err := stageBackupToFile(ctx, file, tempPath); err != nil {
		return result, err
	}
	if err := validateSQLiteBackup(tempPath); err != nil {
		_ = os.Remove(tempPath)
		return result, err
	}
	if err := restorestate.Begin(dbPath, rehearsal.BackupDigest); err != nil {
		cleanupRestoreFile(tempPath)
		return result, common.NewErrorf("Error journaling staged restore: %v", err)
	}
	if err := dbsqlite.CloseForFileSwap(ctx); err != nil {
		cancelErr := restorestate.CancelStaged(dbPath)
		if dbsqlite.DB() == nil {
			return result, reopenLiveDBAfterImportError(dbPath, "closing live db for restore", errors.Join(err, cancelErr))
		}
		return result, common.NewErrorf("Error closing live db for restore: %v", errors.Join(err, cancelErr))
	}
	if err := restorestate.Transition(dbPath, restorestate.StateStaged, restorestate.StateLiveMovePending); err != nil {
		return result, reopenLiveDBAfterImportError(dbPath, "journaling live database move", err)
	}
	if err := os.Rename(dbPath, fallbackPath); err != nil {
		recoverErr := restorestate.Recover(dbPath)
		return result, reopenLiveDBAfterImportError(dbPath, "backing up live db file", errors.Join(err, recoverErr))
	}
	cleanupBackupSidecars(dbPath)
	if err := restorestate.Transition(dbPath, restorestate.StateLiveMovePending, restorestate.StateCandidatePending); err != nil {
		return result, rollbackImportedDB(dbPath, "journaling imported database install", err)
	}
	if err := os.Rename(tempPath, dbPath); err != nil {
		return result, rollbackImportedDB(dbPath, "installing imported db file", err)
	}
	cleanupBackupSidecars(dbPath)

	rollback := func(stage string, cause error) error {
		return rollbackImportedDB(dbPath, stage, cause)
	}
	if err := runImportPostActions(ctx, importRollbackProtectedPostActions(dbPath), rollback); err != nil {
		return result, err
	}
	return result, nil
}

// CompletePendingRestore accepts the candidate only after the caller has
// persisted any operation authority that must survive with it. Cleanup and
// restart failures are projected as pending work because the committed
// candidate is already authoritative and startup recovery can finish cleanup.
func CompletePendingRestore(ctx context.Context) (cleanupPending, restartPending bool, err error) {
	dbPath := configstorage.GetDBPath()
	if err := restorestate.MarkCommitted(dbPath); err != nil {
		return false, false, common.NewErrorf("Error accepting imported database: %v", err)
	}
	if err := restorestate.FinalizeCommitted(dbPath); err != nil {
		cleanupPending = true
	}
	if err := runImportPostActions(ctx, importFinalPostActions(), nil); err != nil {
		restartPending = true
	}
	return cleanupPending, restartPending, nil
}

// AbortPendingRestore returns to the exact pre-restore database while the
// candidate is still rollback-authorized.
func AbortPendingRestore() error {
	dbPath := configstorage.GetDBPath()
	if err := dbsqlite.Close(); err != nil {
		return common.NewErrorf("Error closing rejected imported db: %v", err)
	}
	if err := restorestate.Rollback(dbPath); err != nil {
		return common.NewErrorf("Error restoring exact fallback db: %v", err)
	}
	if err := dbsqlite.Init(dbPath); err != nil {
		return common.NewErrorf("Error reopening exact fallback db: %v", err)
	}
	return nil
}

func cleanupRestoreFile(path string) {
	_ = os.Remove(path)
	cleanupBackupSidecars(path)
}

func rollbackImportedDB(dbPath, stage string, cause error) error {
	if err := dbsqlite.Close(); err != nil {
		return common.NewErrorf("Error %s (%v) and closing imported db for rollback failed: %v; restore recovery remains pending", stage, cause, err)
	}
	if err := restorestate.Rollback(dbPath); err != nil {
		return common.NewErrorf("Error %s (%v) and restoring fallback failed: %v", stage, cause, err)
	}
	return reopenLiveDBAfterImportError(dbPath, stage, cause)
}

func reopenLiveDBAfterImportError(dbPath, stage string, cause error) error {
	if err := dbsqlite.Init(dbPath); err != nil {
		return common.NewErrorf("Error %s (%v) and reopening live db failed: %v", stage, cause, err)
	}
	return common.NewErrorf("Error %s: %v", stage, cause)
}

func stageBackupToFile(ctx context.Context, src io.Reader, dst string) error {
	out, err := os.Create(dst) // #nosec G304 -- internal staging path.
	if err != nil {
		return common.NewErrorf("Error creating temporary db file: %v", err)
	}
	written, copyErr := copyContext(ctx, out, io.LimitReader(src, MaxRestoreBytes+1))
	if copyErr != nil || written <= 0 || written > MaxRestoreBytes {
		_ = out.Close()
		_ = os.Remove(dst)
		return common.NewErrorf("Error saving bounded db: %v", copyErr)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return common.NewErrorf("Error syncing db: %v", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return common.NewErrorf("Error closing temporary db file: %v", err)
	}
	return nil
}

func preservePreRestoreBackup(ctx context.Context) (string, error) {
	path, cleanup, err := PrepareExportContext(ctx, "")
	if err != nil {
		return "", err
	}
	defer cleanup()
	input, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer input.Close()
	directory := filepath.Join(configstorage.GetDBFolderPath(), "recovery", "restore")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(directory, "pre-restore-*.partial")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	hash := sha256.New()
	written, copyErr := copyContext(ctx, io.MultiWriter(temporary, hash), io.LimitReader(input, MaxRestoreBytes+1))
	syncErr, closeErr := temporary.Sync(), temporary.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || written <= 0 || written > MaxRestoreBytes {
		_ = os.Remove(temporaryPath)
		return "", errors.Join(copyErr, syncErr, closeErr, errors.New("pre-restore backup exceeded bounds"))
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	destination := filepath.Join(directory, "pre-restore-"+digest+".db")
	if _, statErr := os.Stat(destination); statErr == nil {
		_ = os.Remove(temporaryPath)
		return digest, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		_ = os.Remove(temporaryPath)
		return "", statErr
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		_ = os.Remove(temporaryPath)
		return "", err
	}
	return digest, nil
}
