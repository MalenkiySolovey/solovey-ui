package backup

import (
	"context"
	canonical "github.com/MalenkiySolovey/solovey-backend/storage/backup"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"io"
	"mime/multipart"
	"path/filepath"
)

func backupRestorer(admission restoreAdmission) canonical.Restorer {
	return canonical.Restorer{
		OpenDatabase: dbsqlite.InitContext,
		DatabasePath: configstorage.GetDBPath,
		Rehearse: func(ctx context.Context, file io.ReadSeeker) (RestoreRehearsal, error) {
			return rehearse(ctx, file, admission)
		},
		ValidateBackup: validateSQLiteBackup,
		PrepareExport:  PrepareExportContext,
		MigrateOwners: func(ctx context.Context, candidate string, statuses []RestoreOwnerStatus, files ...*FileBackupManifest) ([]RestoreOwnerStatus, error) {
			return migrateAndNormalizeRestoreOwners(ctx, candidate, statuses, admission, nil, files...)
		},
		NormalizeOwners:   normalizeRestoredOwners,
		RestoreOwnerFiles: restoreOwnerFiles,
		Restart:           SendSighup,
	}
}

func restoreRecoveryRoot() string {
	return filepath.Join(configstorage.GetDBFolderPath(), "recovery", "restore")
}

func Restore(file multipart.File) error { return RestoreContext(context.Background(), file) }
func RestoreContext(ctx context.Context, file multipart.File) error {
	return backupRestorer(runtimeReady).Restore(ctx, file, restoreRecoveryRoot())
}
func RestoreContextDetailed(ctx context.Context, file io.ReadSeeker) (RestoreExecutionResult, error) {
	return RestoreContextDetailedWithRecoveryRoot(ctx, file, restoreRecoveryRoot())
}
func RestoreContextDetailedWithRecoveryRoot(ctx context.Context, file io.ReadSeeker, recoveryRoot string) (RestoreExecutionResult, error) {
	return backupRestorer(runtimeReady).Detailed(ctx, file, recoveryRoot)
}

// RestoreOperatorDetailed is the supervised import path, not a recovery
// readiness check for signed updates or platform preservation.
func RestoreOperatorDetailed(ctx context.Context, file io.ReadSeeker, recoveryRoot string) (RestoreExecutionResult, error) {
	return backupRestorer(operatorRestore).Detailed(ctx, file, recoveryRoot)
}
func CompletePendingRestore(ctx context.Context) (bool, bool, error) {
	return backupRestorer(runtimeReady).Complete(ctx)
}
func AbortPendingRestore() error { return backupRestorer(runtimeReady).Abort() }
