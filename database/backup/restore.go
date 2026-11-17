package backup

import (
	"context"
	canonical "github.com/MalenkiySolovey/solovey-ui/backend/storage/backup"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"io"
	"mime/multipart"
	"path/filepath"
)

func backupRestorer() canonical.Restorer {
	return canonical.Restorer{
		OpenDatabase:      dbsqlite.InitContext,
		DatabasePath:      configstorage.GetDBPath,
		Rehearse:          Rehearse,
		ValidateBackup:    validateSQLiteBackup,
		PrepareExport:     PrepareExportContext,
		MigrateOwners:     migrateAndNormalizeRestoreOwners,
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
	return backupRestorer().Restore(ctx, file, restoreRecoveryRoot())
}
func RestoreContextDetailed(ctx context.Context, file io.ReadSeeker) (RestoreExecutionResult, error) {
	return backupRestorer().Detailed(ctx, file, restoreRecoveryRoot())
}
func RestoreContextDetailedWithRecoveryRoot(ctx context.Context, file io.ReadSeeker, recoveryRoot string) (RestoreExecutionResult, error) {
	return backupRestorer().Detailed(ctx, file, recoveryRoot)
}
func CompletePendingRestore(ctx context.Context) (bool, bool, error) {
	return backupRestorer().Complete(ctx)
}
func AbortPendingRestore() error { return backupRestorer().Abort() }
