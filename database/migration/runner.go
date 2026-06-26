package migration

import (
	"fmt"
	"os"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	"github.com/MalenkiySolovey/solovey-ui/database/migration/integrity"
	"github.com/MalenkiySolovey/solovey-ui/database/migration/steps"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Options struct {
	RepairForeignKeyOrphans bool
}

// MigrateDb runs schema migrations against the SQLite database located at
// `configstorage.GetDBPath()`. The legacy variant terminated the process on any
// error, which made restoring an incompatible backup through the panel kill
// the whole panel. The function now returns an error so callers can decide
// what to do (the CLI prints and exits non-zero, the panel falls back to the
// previous database).
func MigrateDb() error {
	return MigrateDbWithOptions(Options{})
}

func MigrateDbWithOptions(options Options) error {
	// void running on first install
	path := configstorage.GetDBPath()
	if _, err := os.Stat(path); err != nil {
		fmt.Println("Database not found")
		return nil
	}

	db, err := gorm.Open(sqlite.Open(sqliteMigrationDSN(path)))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("db handle: %w", err)
	}
	defer sqlDB.Close()

	if err := integrity.EnsureNoTLSForeignKeyParent(db); err != nil {
		return err
	}
	if err := integrity.VerifyForeignKeysBeforeMigration(db, integrity.Options{
		RepairForeignKeyOrphans: options.RepairForeignKeyOrphans,
	}); err != nil {
		return err
	}

	tx := db.Begin()
	if tx.Error != nil {
		return fmt.Errorf("begin migration: %w", tx.Error)
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	currentVersion := configidentity.GetVersion()
	dbVersion := ""
	tx.Raw("SELECT value FROM settings WHERE key = ?", "version").Find(&dbVersion)
	fmt.Println("Current version:", currentVersion, "\nDatabase version:", dbVersion)

	if currentVersion == dbVersion {
		fmt.Println("Database is up to date, no need to migrate")
		return nil
	}
	if dbVersion != "" {
		cmp, ok := versionpolicy.CompareVersions(dbVersion, currentVersion)
		if !ok {
			return fmt.Errorf("database version %q is not semver-compatible", dbVersion)
		}
		if cmp > 0 {
			fmt.Println("Database version is newer than current binary, no migration will run")
			return nil
		}
	}

	fmt.Println("Start migrating database...")

	if dbVersion, err = steps.RunPending(tx, dbVersion); err != nil {
		return err
	}

	// Persist the new version. The settings row is created lazily in older
	// schemas, so use UPSERT semantics.
	var count int64
	if err = tx.Raw("SELECT COUNT(*) FROM settings WHERE key = ?", "version").Scan(&count).Error; err != nil {
		return fmt.Errorf("count version: %w", err)
	}
	if count == 0 {
		err = tx.Exec("INSERT INTO settings(key, value) VALUES(?, ?)", "version", currentVersion).Error
	} else {
		err = tx.Exec("UPDATE settings SET value = ? WHERE key = ?", currentVersion, "version").Error
	}
	if err != nil {
		return fmt.Errorf("update version: %w", err)
	}
	if err = tx.Commit().Error; err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	committed = true
	if err = checkpointWAL(db); err != nil {
		fmt.Println("Warning: WAL checkpoint skipped:", err)
	}
	fmt.Println("Migration done!")
	return nil
}
