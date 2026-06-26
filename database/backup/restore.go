package backup

import (
	"context"
	"io"
	"mime/multipart"
	"os"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

// Restore validates and atomically installs an uploaded Solovey UI database.
func Restore(file multipart.File) error {
	valid, err := IsSQLite(file)
	if err != nil {
		return common.NewErrorf("Error checking db file format: %v", err)
	}
	if !valid {
		return common.NewError("Invalid db file format")
	}
	if _, err = file.Seek(0, 0); err != nil {
		return common.NewErrorf("Error resetting file reader: %v", err)
	}

	dbPath := configstorage.GetDBPath()
	tempPath := dbPath + ".temp"
	fallbackPath := dbPath + ".backup"
	cleanupRestoreFile(tempPath)
	cleanupRestoreFile(fallbackPath)
	if err := stageBackupToFile(file, tempPath); err != nil {
		return err
	}
	if err := validateSQLiteBackup(tempPath); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	_ = dbsqlite.Close()

	fallbackReady := false
	if _, statErr := os.Stat(dbPath); statErr == nil {
		if err := os.Rename(dbPath, fallbackPath); err != nil {
			return reopenLiveDBAfterImportError(dbPath, "backing up live db file", err)
		}
		fallbackReady = true
	} else if !os.IsNotExist(statErr) {
		return reopenLiveDBAfterImportError(dbPath, "checking live db file", statErr)
	}
	cleanupBackupSidecars(dbPath)
	if err := os.Rename(tempPath, dbPath); err != nil {
		return rollbackImportedDB(dbPath, fallbackPath, fallbackReady, "installing imported db file", err)
	}
	cleanupBackupSidecars(dbPath)

	rollback := func(stage string, cause error) error {
		return rollbackImportedDB(dbPath, fallbackPath, fallbackReady, stage, cause)
	}
	if err := runImportPostActions(context.Background(), importRollbackProtectedPostActions(dbPath), rollback); err != nil {
		return err
	}
	cleanupRestoreFile(fallbackPath)
	return runImportPostActions(context.Background(), importFinalPostActions(), nil)
}

func cleanupRestoreFile(path string) {
	_ = os.Remove(path)
	cleanupBackupSidecars(path)
}

func rollbackImportedDB(dbPath, fallbackPath string, fallbackReady bool, stage string, cause error) error {
	_ = dbsqlite.Close()
	cleanupRestoreFile(dbPath)
	if !fallbackReady {
		return common.NewErrorf("Error %s: %v", stage, cause)
	}
	if err := os.Rename(fallbackPath, dbPath); err != nil {
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

func stageBackupToFile(src io.Reader, dst string) error {
	out, err := os.Create(dst) // #nosec G304 -- internal staging path.
	if err != nil {
		return common.NewErrorf("Error creating temporary db file: %v", err)
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return common.NewErrorf("Error saving db: %v", err)
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
