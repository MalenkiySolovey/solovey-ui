package migration

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

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
	return MigratePath(path, options)
}

// MigratePath runs the same production migration plan against an explicitly
// selected database. Restore rehearsal uses it only on a restrictive,
// disposable same-filesystem copy; it never changes the configured live path.
func MigratePath(path string, options Options) error {
	if path == "" {
		return fmt.Errorf("migration database path is required")
	}
	currentVersion := configidentity.GetVersion()
	preflightDBVersion, preflightCoreVersion, err := readOnlyVersionPreflight(path)
	if err != nil {
		return err
	}
	if err := rejectFutureVersion("database", preflightDBVersion, currentVersion); err != nil {
		return err
	}
	if err := rejectFutureVersion("core schema", preflightCoreVersion, "1.11"); err != nil {
		return err
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

	dbVersion, err := readVersionSetting(db, "version")
	if err != nil {
		return err
	}
	coreVersion, err := readVersionSetting(db, "coreSchemaVersion")
	if err != nil {
		return err
	}
	if err := rejectFutureVersion("database", dbVersion, currentVersion); err != nil {
		return err
	}
	if err := rejectFutureVersion("core schema", coreVersion, "1.11"); err != nil {
		return err
	}
	if dbVersion != preflightDBVersion || coreVersion != preflightCoreVersion {
		return errors.New("database version changed after the read-only migration preflight")
	}
	fmt.Println("Current version:", currentVersion, "\nDatabase version:", dbVersion, "\nCore schema version:", coreVersion)
	if currentVersion == dbVersion && coreVersion == "1.11" {
		if err := validateOperationsMigrationJournal(db); err != nil {
			return err
		}
		fmt.Println("Database is up to date, no need to migrate")
		return nil
	}
	if coreVersion == "1.11" {
		if err := validateOperationsMigrationJournal(db); err != nil {
			return err
		}
	}

	if err := integrity.EnsureNoTLSForeignKeyParent(db); err != nil {
		return err
	}
	if err := integrity.VerifyForeignKeysBeforeMigration(db, integrity.Options{
		RepairForeignKeyOrphans: options.RepairForeignKeyOrphans,
	}); err != nil {
		return err
	}

	coreComparison, coreComparable := versionpolicy.CompareVersions(coreVersionOrBaseline(coreVersion), "1.11")
	if !coreComparable {
		return errors.New("core schema version is not migration-compatible")
	}
	journalPending := coreComparison < 0
	if journalPending {
		if err := ensureOperationsMigrationJournal(db); err != nil {
			return err
		}
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

	fmt.Println("Start migrating database...")

	if _, err = steps.RunPending(tx, dbVersion); err != nil {
		if journalPending {
			_ = recordOperationsMigrationState(db, "FAILED", "core_migration_failed")
		}
		return err
	}
	if coreVersion, err = steps.RunCorePending(tx, coreVersion); err != nil {
		if journalPending {
			_ = recordOperationsMigrationState(db, "FAILED", "core_migration_failed")
		}
		return err
	}
	if err = upsertVersionSetting(tx, "coreSchemaVersion", coreVersion); err != nil {
		return fmt.Errorf("update core schema version: %w", err)
	}

	// Persist the new version. The settings row is created lazily in older
	// schemas, so use UPSERT semantics.
	if err = upsertVersionSetting(tx, "version", currentVersion); err != nil {
		return fmt.Errorf("update version: %w", err)
	}
	if err = tx.Commit().Error; err != nil {
		if journalPending {
			_ = recordOperationsMigrationState(db, "RECOVERY_REQUIRED", "core_migration_commit_ambiguous")
		}
		return fmt.Errorf("commit migration: %w", err)
	}
	committed = true
	if journalPending {
		if err = recordOperationsMigrationState(db, "APPLIED", ""); err != nil {
			return fmt.Errorf("finalize core migration journal: %w", err)
		}
	}
	if err = validateOperationsMigrationJournal(db); err != nil {
		return err
	}
	if err = checkpointWAL(db); err != nil {
		fmt.Println("Warning: WAL checkpoint skipped:", err)
	}
	fmt.Println("Migration done!")
	return nil
}

func readOnlyVersionPreflight(path string) (string, string, error) {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	db, err := gorm.Open(sqlite.Open(path + separator + "mode=ro&_query_only=1&_foreign_keys=on"))
	if err != nil {
		return "", "", fmt.Errorf("open read-only migration preflight: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return "", "", err
	}
	defer sqlDB.Close()
	databaseVersion, err := readVersionSetting(db, "version")
	if err != nil {
		return "", "", err
	}
	coreVersion, err := readVersionSetting(db, "coreSchemaVersion")
	return databaseVersion, coreVersion, err
}

func coreVersionOrBaseline(value string) string {
	if value == "" {
		return "1.7"
	}
	return value
}

func ensureOperationsMigrationJournal(db *gorm.DB) error {
	if err := ensureOperationsMigrationJournalTable(db); err != nil {
		return fmt.Errorf("create core migration journal: %w", err)
	}
	var existing struct {
		Checksum string
		State    string
	}
	err := db.Raw("SELECT checksum, state FROM migration_journal_v1 WHERE scope = ? AND owner_id = ? AND step_id = ?",
		"core", "core", steps.OperationsLifecycleStepID).Row().Scan(&existing.Checksum, &existing.State)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if existing.Checksum != steps.OperationsLifecycleChecksum {
			now := time.Now().Unix()
			_ = db.Exec("UPDATE migration_journal_v1 SET state='RECOVERY_REQUIRED', compatibility_state='CHECKSUM_MISMATCH', error_code='core_migration_checksum_mismatch', finished_at=?, updated_at=? WHERE scope=? AND owner_id=? AND step_id=?",
				now, now, "core", "core", steps.OperationsLifecycleStepID).Error
			return errors.New("core migration checksum changed; recovery is required")
		}
		if existing.State == "RECOVERY_REQUIRED" || existing.State == "APPLIED" {
			return fmt.Errorf("core migration journal state %s is inconsistent with schema 1.10", existing.State)
		}
	}
	return recordOperationsMigrationState(db, "RUNNING", "")
}

func recordOperationsMigrationState(db *gorm.DB, state, errorCode string) error {
	now := time.Now().Unix()
	finishedAt := int64(0)
	if state == "APPLIED" || state == "FAILED" || state == "RECOVERY_REQUIRED" {
		finishedAt = now
	}
	result := db.Exec(`INSERT INTO migration_journal_v1
		(scope, owner_id, step_id, checksum, state, compatibility_state, retry_count, error_code, backup_ref, restore_ref, drop_state, started_at, finished_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?, '', '', 'NOT_REQUESTED', ?, ?, ?)
		ON CONFLICT(scope, owner_id, step_id) DO UPDATE SET
		state=excluded.state, compatibility_state=excluded.compatibility_state,
		error_code=excluded.error_code, finished_at=excluded.finished_at, updated_at=excluded.updated_at,
		retry_count=CASE WHEN excluded.state='RUNNING' THEN migration_journal_v1.retry_count + 1 ELSE migration_journal_v1.retry_count END
		WHERE migration_journal_v1.checksum=excluded.checksum`,
		"core", "core", steps.OperationsLifecycleStepID, steps.OperationsLifecycleChecksum,
		state, "COMPATIBLE", errorCode, now, finishedAt, now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("core migration journal checksum or state changed")
	}
	return nil
}

// EnsureCurrentSchemaJournal seeds only a freshly bootstrapped current schema.
// Existing or migrated databases must already carry the exact applied row.
func EnsureCurrentSchemaJournal(db *gorm.DB, seedFresh bool) error {
	if db == nil {
		return errors.New("core migration journal database is unavailable")
	}
	coreVersion, err := readVersionSetting(db, "coreSchemaVersion")
	if err != nil {
		return err
	}
	if coreVersion == "" && seedFresh {
		if err := upsertVersionSetting(db, "coreSchemaVersion", "1.11"); err != nil {
			return err
		}
		coreVersion = "1.11"
	}
	if coreVersion != "1.11" {
		return fmt.Errorf("cannot seed current migration journal for core schema %q", coreVersion)
	}
	if err := ensureOperationsMigrationJournalTable(db); err != nil {
		return err
	}
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM migration_journal_v1 WHERE scope=? AND owner_id=? AND step_id=?",
		"core", "core", steps.OperationsLifecycleStepID).Scan(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		now := time.Now().Unix()
		if err := db.Exec(`INSERT INTO migration_journal_v1
			(scope, owner_id, step_id, checksum, state, compatibility_state, retry_count, error_code, backup_ref, restore_ref, drop_state, started_at, finished_at, updated_at)
			VALUES (?, ?, ?, ?, 'APPLIED', 'COMPATIBLE', 0, '', '', '', 'NOT_REQUESTED', ?, ?, ?)`,
			"core", "core", steps.OperationsLifecycleStepID, steps.OperationsLifecycleChecksum, now, now, now).Error; err != nil {
			return err
		}
	}
	return validateOperationsMigrationJournal(db)
}

func validateOperationsMigrationJournal(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable("migration_journal_v1") {
		return errors.New("current core schema migration journal is absent")
	}
	var row struct {
		Checksum string
		State    string
	}
	err := db.Raw("SELECT checksum, state FROM migration_journal_v1 WHERE scope=? AND owner_id=? AND step_id=? LIMIT 1",
		"core", "core", steps.OperationsLifecycleStepID).Scan(&row).Error
	if err != nil {
		return err
	}
	if row.Checksum != steps.OperationsLifecycleChecksum || row.State != "APPLIED" {
		return errors.New("current core schema migration journal is not exactly applied")
	}
	return nil
}

func ensureOperationsMigrationJournalTable(db *gorm.DB) error {
	return db.Exec(`CREATE TABLE IF NOT EXISTS migration_journal_v1 (
		scope TEXT NOT NULL, owner_id TEXT NOT NULL, step_id TEXT NOT NULL, checksum TEXT NOT NULL,
		state TEXT NOT NULL, compatibility_state TEXT NOT NULL, retry_count INTEGER NOT NULL DEFAULT 0,
		error_code TEXT NOT NULL, backup_ref TEXT NOT NULL, restore_ref TEXT NOT NULL, drop_state TEXT NOT NULL,
		started_at INTEGER NOT NULL, finished_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
		PRIMARY KEY (scope, owner_id, step_id))`).Error
}

func readVersionSetting(db *gorm.DB, key string) (string, error) {
	if !db.Migrator().HasTable("settings") {
		return "", nil
	}
	var value string
	result := db.Raw("SELECT value FROM settings WHERE key = ? LIMIT 1", key).Scan(&value)
	if result.Error != nil {
		return "", fmt.Errorf("read %s: %w", key, result.Error)
	}
	return value, nil
}

func rejectFutureVersion(label, actual, supported string) error {
	if actual == "" {
		return nil
	}
	cmp, ok := versionpolicy.CompareVersions(actual, supported)
	if !ok {
		return fmt.Errorf("%s version %q is not semver-compatible", label, actual)
	}
	if cmp > 0 {
		return fmt.Errorf("%s version %q is newer than supported %q", label, actual, supported)
	}
	return nil
}

func upsertVersionSetting(tx *gorm.DB, key, value string) error {
	var count int64
	if err := tx.Raw("SELECT COUNT(*) FROM settings WHERE key = ?", key).Scan(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return tx.Exec("INSERT INTO settings(key, value) VALUES(?, ?)", key, value).Error
	}
	return tx.Exec("UPDATE settings SET value = ? WHERE key = ?", value, key).Error
}
