package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/hooks"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/restorestate"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"gorm.io/gorm"
)

// Restorer coordinates staged database installation through the canonical SQLite
// lifetime and restore journal. Composition supplies the existing semantic owners.
type Restorer struct {
	OpenDatabase      func(context.Context, string) error
	DatabasePath      func() string
	Rehearse          func(context.Context, io.ReadSeeker) (RestoreRehearsal, error)
	ValidateBackup    func(string) error
	PrepareExport     func(context.Context, string) (string, func(), error)
	MigrateOwners     func(context.Context, string, []RestoreOwnerStatus, ...*FileBackupManifest) ([]RestoreOwnerStatus, error)
	NormalizeOwners   func(context.Context, *gorm.DB, []RestoreOwnerStatus) ([]RestoreOwnerStatus, error)
	RestoreOwnerFiles func(context.Context, *gorm.DB, *FileBackupManifest, bool) error
	Restart           func() error
}

func (r Restorer) available() bool {
	return r.OpenDatabase != nil && r.DatabasePath != nil && r.Rehearse != nil && r.ValidateBackup != nil && r.PrepareExport != nil && r.MigrateOwners != nil && r.NormalizeOwners != nil && r.RestoreOwnerFiles != nil && r.Restart != nil
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

// Restore validates, installs and accepts a candidate when no caller authority
// remains to persist. Detailed keeps admission private until Complete or Abort.
func (r Restorer) Restore(ctx context.Context, file io.ReadSeeker, recoveryRoot string) error {
	_, err := r.Detailed(ctx, file, recoveryRoot)
	if err != nil {
		return err
	}
	_, _, err = r.Complete(ctx)
	return err
}

func (r Restorer) Detailed(ctx context.Context, file io.ReadSeeker, recoveryRoot string) (RestoreExecutionResult, error) {
	result := RestoreExecutionResult{}
	if ctx == nil || strings.TrimSpace(recoveryRoot) == "" {
		return result, errors.New("Restore context is required")
	}
	if !r.available() {
		return result, errors.New("restore owner composition is unavailable")
	}
	rehearsal, rehearsalErr := r.Rehearse(ctx, file)
	result.Rehearsal = rehearsal
	if rehearsalErr != nil {
		return result, fmt.Errorf("Restore rehearsal rejected the backup: %v", rehearsalErr)
	}
	if !rehearsal.Possible {
		return result, fmt.Errorf("Restore rehearsal rejected the backup: %s", strings.Join(rehearsal.ReasonCodes, ","))
	}
	valid, err := IsSQLite(file)
	if err != nil {
		return result, fmt.Errorf("Error checking db file format: %v", err)
	}
	if !valid {
		return result, errors.New("Invalid db file format")
	}
	if _, err = file.Seek(0, 0); err != nil {
		return result, fmt.Errorf("Error resetting file reader: %v", err)
	}

	dbPath := r.DatabasePath()
	tempPath := restorestate.StagingPath(dbPath)
	fallbackPath := restorestate.FallbackPath(dbPath)
	if err := restorestate.EnsureIdle(dbPath); err != nil {
		return result, fmt.Errorf("Database restore recovery is required: %v", err)
	}
	owner, err := dbsqlite.BeginMaintenance(ctx)
	if err != nil {
		return result, err
	}
	result = result.WithMaintenance(owner)
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
	if result.RecoveryBackupRef, err = r.preservePreRestoreBackupAt(ctx, recoveryRoot, productionRestoreRecoveryFileOps); err != nil {
		return result, fmt.Errorf("Error preserving pre-restore recovery backup: %v", err)
	}
	if err := stageBackupToFile(ctx, file, tempPath); err != nil {
		return result, err
	}
	if err := r.ValidateBackup(tempPath); err != nil {
		_ = os.Remove(tempPath)
		return result, err
	}
	if err := restorestate.Begin(dbPath, rehearsal.BackupDigest); err != nil {
		CleanupStagedFile(tempPath)
		return result, fmt.Errorf("Error journaling staged restore: %v", err)
	}
	if err := dbsqlite.CloseForFileSwap(ctx); err != nil {
		cancelErr := restorestate.CancelStaged(dbPath)
		if !dbsqlite.IsOpen() {
			keepPrivate = true
			return result, r.reopenLiveDBAfterImportError(ctx, dbPath, "closing live db for restore", errors.Join(err, cancelErr))
		}
		return result, fmt.Errorf("Error closing live db for restore: %v", errors.Join(err, cancelErr))
	}
	keepPrivate = true
	if err := restorestate.Transition(dbPath, restorestate.StateStaged, restorestate.StateLiveMovePending); err != nil {
		return result, r.reopenLiveDBAfterImportError(ctx, dbPath, "journaling live database move", err)
	}
	if err := os.Rename(dbPath, fallbackPath); err != nil {
		recoverErr := restorestate.Recover(dbPath)
		return result, r.reopenLiveDBAfterImportError(ctx, dbPath, "backing up live db file", errors.Join(err, recoverErr))
	}
	CleanupSidecars(dbPath)
	if err := restorestate.Transition(dbPath, restorestate.StateLiveMovePending, restorestate.StateCandidatePending); err != nil {
		return result, r.rollbackImportedDB(ctx, dbPath, "journaling imported database install", err)
	}
	if err := os.Rename(tempPath, dbPath); err != nil {
		return result, r.rollbackImportedDB(ctx, dbPath, "installing imported db file", err)
	}
	CleanupSidecars(dbPath)

	rollback := func(stage string, cause error) error {
		return r.rollbackImportedDB(ctx, dbPath, stage, cause)
	}
	if err := runImportPostActions(ctx, r.importRollbackProtectedPostActions(dbPath, rehearsal.Owners, rehearsal.Manifest.Files), rollback); err != nil {
		return result, err
	}
	return result, nil
}

// Complete accepts the candidate only after the caller has
// persisted any operation authority that must survive with it. Cleanup and
// restart failures are projected as pending work because the committed
// candidate is already authoritative and startup recovery can finish cleanup.
func (r Restorer) Complete(ctx context.Context) (cleanupPending, restartPending bool, err error) {
	dbPath := r.DatabasePath()
	if err := restorestate.MarkCommitted(dbPath); err != nil {
		return false, false, fmt.Errorf("Error accepting imported database: %v", err)
	}
	// The protected rebind and caller authority write have completed. Publish
	// before restart so no background owner waits for a scope held by restart.
	finishMaintenance(pendingMaintenance())
	if err := restorestate.FinalizeCommitted(dbPath); err != nil {
		cleanupPending = true
	}
	if err := runImportPostActions(ctx, r.importFinalPostActions(), nil); err != nil {
		restartPending = true
	}
	return cleanupPending, restartPending, nil
}

// Abort returns to the exact pre-restore database while the
// candidate is still rollback-authorized.
func (r Restorer) Abort() error {
	ctx, cancel := restoreRecoveryContext(context.Background())
	defer cancel()
	dbPath := r.DatabasePath()
	if err := dbsqlite.CloseContext(ctx); err != nil {
		return fmt.Errorf("Error closing rejected imported db: %v", err)
	}
	if err := restorestate.Rollback(dbPath); err != nil {
		return fmt.Errorf("Error restoring exact fallback db: %v", err)
	}
	if err := r.OpenDatabase(ctx, dbPath); err != nil {
		return fmt.Errorf("Error reopening exact fallback db: %v", err)
	}
	if err := resetReopenedDatabaseCaches(ctx); err != nil {
		return err
	}
	finishMaintenance(pendingMaintenance())
	return nil
}

// CleanupStagedFile removes an internal candidate and its SQLite sidecars.
func CleanupStagedFile(path string) {
	_ = os.Remove(path)
	CleanupSidecars(path)
}

func (r Restorer) rollbackImportedDB(ctx context.Context, dbPath, stage string, cause error) error {
	recoveryCtx, cancel := restoreRecoveryContext(ctx)
	defer cancel()
	if err := dbsqlite.CloseContext(recoveryCtx); err != nil {
		return fmt.Errorf("Error %s (%v) and closing imported db for rollback failed: %v; restore recovery remains pending", stage, cause, err)
	}
	if err := restorestate.Rollback(dbPath); err != nil {
		return fmt.Errorf("Error %s (%v) and restoring fallback failed: %v", stage, cause, err)
	}
	return r.reopenLiveDBAfterImportError(recoveryCtx, dbPath, stage, cause)
}

func (r Restorer) reopenLiveDBAfterImportError(ctx context.Context, dbPath, stage string, cause error) error {
	recoveryCtx, cancel := restoreRecoveryContext(ctx)
	defer cancel()
	if err := r.OpenDatabase(recoveryCtx, dbPath); err != nil {
		return fmt.Errorf("Error %s (%v) and reopening live db failed: %v", stage, cause, err)
	}
	if err := resetReopenedDatabaseCaches(recoveryCtx); err != nil {
		return fmt.Errorf("Error %s (%v) and rebinding live database owners failed: %v", stage, cause, err)
	}
	finishMaintenance(pendingMaintenance())
	return fmt.Errorf("Error %s: %v", stage, cause)
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
		return fmt.Errorf("Error creating temporary db file: %v", err)
	}
	written, copyErr := CopyContext(ctx, out, io.LimitReader(src, MaxRestoreBytes+1))
	if copyErr != nil || written <= 0 || written > MaxRestoreBytes {
		_ = out.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("Error saving bounded db: %v", copyErr)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("Error syncing db: %v", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("Error closing temporary db file: %v", err)
	}
	return nil
}

func (r Restorer) preservePreRestoreBackupAt(ctx context.Context, directory string, ops restoreRecoveryFileOps) (string, error) {
	path, cleanup, err := r.PrepareExport(ctx, "")
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
	written, copyErr := CopyContext(ctx, io.MultiWriter(temporary, hash), io.LimitReader(input, MaxRestoreBytes+1))
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

// PreserveRecovery publishes and verifies a content-addressed pre-restore archive.
func (r Restorer) PreserveRecovery(ctx context.Context, directory string) (string, error) {
	if r.PrepareExport == nil {
		return "", errors.New("backup exporter is unavailable")
	}
	return r.preservePreRestoreBackupAt(ctx, directory, productionRestoreRecoveryFileOps)
}
