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
	"sync"
	"time"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/hooks"
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
	maintenance            *dbsqlite.Maintenance
}

// DatabaseContext lets the restore caller persist its operation authority
// against the private candidate. It is temporary and is never a cached DB.
func (r RestoreExecutionResult) DatabaseContext(ctx context.Context) context.Context {
	if r.maintenance != nil && r.maintenance.Active() {
		return r.maintenance.Context(ctx)
	}
	return ctx
}

var pendingRestoreMaintenance = struct {
	sync.Mutex
	owner *dbsqlite.Maintenance
}{}

func pendingMaintenance() *dbsqlite.Maintenance {
	pendingRestoreMaintenance.Lock()
	defer pendingRestoreMaintenance.Unlock()
	owner := pendingRestoreMaintenance.owner
	if owner != nil && owner.Active() {
		return owner
	}
	return nil
}

func finishMaintenance(owner *dbsqlite.Maintenance) {
	if owner == nil {
		return
	}
	pendingRestoreMaintenance.Lock()
	if pendingRestoreMaintenance.owner == owner {
		pendingRestoreMaintenance.owner = nil
	}
	pendingRestoreMaintenance.Unlock()
	owner.End()
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
	return RestoreContextDetailedWithRecoveryRoot(ctx, file, filepath.Join(configstorage.GetDBFolderPath(), "recovery", "restore"))
}

// RestoreContextDetailedWithRecoveryRoot keeps the recovery-file location an
// injected owner fact for data-lifecycle filesystem tests while preserving the
// configured database root as the production default.
func RestoreContextDetailedWithRecoveryRoot(ctx context.Context, file io.ReadSeeker, recoveryRoot string) (RestoreExecutionResult, error) {
	result := RestoreExecutionResult{}
	if ctx == nil || strings.TrimSpace(recoveryRoot) == "" {
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
	owner, err := dbsqlite.BeginMaintenance(ctx)
	if err != nil {
		return result, err
	}
	result.maintenance = owner
	pendingRestoreMaintenance.Lock()
	pendingRestoreMaintenance.owner = owner
	pendingRestoreMaintenance.Unlock()
	ctx = owner.Context(ctx)
	keepPrivate := false
	defer func() {
		if !keepPrivate {
			finishMaintenance(owner)
		}
	}()
	// Repeat under the same maintenance admission that owns the staging path.
	if err := restorestate.EnsureIdle(dbPath); err != nil {
		return result, err
	}
	if result.RecoveryBackupRef, err = preservePreRestoreBackupAt(ctx, recoveryRoot, productionRestoreRecoveryFileOps); err != nil {
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
		if !dbsqlite.IsOpen() {
			keepPrivate = true
			return result, reopenLiveDBAfterImportError(ctx, dbPath, "closing live db for restore", errors.Join(err, cancelErr))
		}
		return result, common.NewErrorf("Error closing live db for restore: %v", errors.Join(err, cancelErr))
	}
	keepPrivate = true
	if err := restorestate.Transition(dbPath, restorestate.StateStaged, restorestate.StateLiveMovePending); err != nil {
		return result, reopenLiveDBAfterImportError(ctx, dbPath, "journaling live database move", err)
	}
	if err := os.Rename(dbPath, fallbackPath); err != nil {
		recoverErr := restorestate.Recover(dbPath)
		return result, reopenLiveDBAfterImportError(ctx, dbPath, "backing up live db file", errors.Join(err, recoverErr))
	}
	cleanupBackupSidecars(dbPath)
	if err := restorestate.Transition(dbPath, restorestate.StateLiveMovePending, restorestate.StateCandidatePending); err != nil {
		return result, rollbackImportedDB(ctx, dbPath, "journaling imported database install", err)
	}
	if err := os.Rename(tempPath, dbPath); err != nil {
		return result, rollbackImportedDB(ctx, dbPath, "installing imported db file", err)
	}
	cleanupBackupSidecars(dbPath)

	rollback := func(stage string, cause error) error {
		return rollbackImportedDB(ctx, dbPath, stage, cause)
	}
	if err := runImportPostActions(ctx, importRollbackProtectedPostActions(dbPath, rehearsal.Owners, rehearsal.Manifest.Files), rollback); err != nil {
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
	// The protected rebind and caller authority write have completed. Publish
	// before restart so no background owner waits for a scope held by restart.
	finishMaintenance(pendingMaintenance())
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
	ctx, cancel := restoreRecoveryContext(context.Background())
	defer cancel()
	dbPath := configstorage.GetDBPath()
	if err := dbsqlite.CloseContext(ctx); err != nil {
		return common.NewErrorf("Error closing rejected imported db: %v", err)
	}
	if err := restorestate.Rollback(dbPath); err != nil {
		return common.NewErrorf("Error restoring exact fallback db: %v", err)
	}
	if err := dbsqlite.InitContext(ctx, dbPath); err != nil {
		return common.NewErrorf("Error reopening exact fallback db: %v", err)
	}
	if err := resetReopenedDatabaseCaches(ctx); err != nil {
		return err
	}
	finishMaintenance(pendingMaintenance())
	return nil
}

func cleanupRestoreFile(path string) {
	_ = os.Remove(path)
	cleanupBackupSidecars(path)
}

func rollbackImportedDB(ctx context.Context, dbPath, stage string, cause error) error {
	recoveryCtx, cancel := restoreRecoveryContext(ctx)
	defer cancel()
	if err := dbsqlite.CloseContext(recoveryCtx); err != nil {
		return common.NewErrorf("Error %s (%v) and closing imported db for rollback failed: %v; restore recovery remains pending", stage, cause, err)
	}
	if err := restorestate.Rollback(dbPath); err != nil {
		return common.NewErrorf("Error %s (%v) and restoring fallback failed: %v", stage, cause, err)
	}
	return reopenLiveDBAfterImportError(recoveryCtx, dbPath, stage, cause)
}

func reopenLiveDBAfterImportError(ctx context.Context, dbPath, stage string, cause error) error {
	recoveryCtx, cancel := restoreRecoveryContext(ctx)
	defer cancel()
	if err := dbsqlite.InitContext(recoveryCtx, dbPath); err != nil {
		return common.NewErrorf("Error %s (%v) and reopening live db failed: %v", stage, cause, err)
	}
	if err := resetReopenedDatabaseCaches(recoveryCtx); err != nil {
		return common.NewErrorf("Error %s (%v) and rebinding live database owners failed: %v", stage, cause, err)
	}
	finishMaintenance(pendingMaintenance())
	return common.NewErrorf("Error %s: %v", stage, cause)
}

func restoreRecoveryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	// The rejected import may have exhausted its request context. Recovery of
	// the reopened fallback has its own bounded lifetime.
	recoveryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if owner := pendingMaintenance(); owner != nil {
		recoveryCtx = owner.Context(recoveryCtx)
	}
	return recoveryCtx, cancel
}

func resetReopenedDatabaseCaches(ctx context.Context) error { return hooks.ResetCaches(ctx) }

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

func preservePreRestoreBackupAt(ctx context.Context, directory string, ops restoreRecoveryFileOps) (string, error) {
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
	if err := ensureRestoreRecoveryDirectory(directory, ops); err != nil {
		return "", err
	}
	temporary, err := ops.createTemp(directory, "pre-restore-*.partial")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	hash := sha256.New()
	written, copyErr := copyContext(ctx, io.MultiWriter(temporary, hash), io.LimitReader(input, MaxRestoreBytes+1))
	syncErr, closeErr := temporary.Sync(), temporary.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || written <= 0 || written > MaxRestoreBytes {
		_ = ops.remove(temporaryPath)
		return "", errors.Join(copyErr, syncErr, closeErr, errors.New("pre-restore backup exceeded bounds"))
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	destination := filepath.Join(directory, "pre-restore-"+digest+".db")
	if _, statErr := ops.stat(destination); statErr == nil {
		_ = ops.remove(temporaryPath)
		if _, err := verifyRestoreRecoveryFile(ctx, destination, digest, ops); err != nil {
			return "", err
		}
		return digest, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		_ = ops.remove(temporaryPath)
		return "", statErr
	}
	if err := ops.rename(temporaryPath, destination); err != nil {
		_ = ops.remove(temporaryPath)
		return "", err
	}
	if err := ops.syncDirectory(directory); err != nil {
		removeErr := ops.remove(destination)
		if removeErr == nil {
			removeErr = ops.syncDirectory(directory)
		}
		return "", errors.Join(err, removeErr)
	}
	if _, err := verifyRestoreRecoveryFile(ctx, destination, digest, ops); err != nil {
		removeErr := ops.remove(destination)
		if removeErr == nil {
			removeErr = ops.syncDirectory(directory)
		}
		return "", errors.Join(err, removeErr)
	}
	return digest, nil
}
