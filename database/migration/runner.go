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
	dbschema "github.com/MalenkiySolovey/solovey-ui/database/schema"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type Options struct {
	RepairForeignKeyOrphans bool
	LegacyConfigPath        string
	ProjectRuntimeFiles     func(*gorm.DB, []byte) ([]byte, error)
	ReportFindings          func([]diagnostics.Finding)
}

const maxLegacyConfigBytes = int64(16 << 20)

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
	path := configstorage.GetDBPath()
	return MigratePathIfExists(path, options)
}

// MigratePathIfExists is the authoritative open-time migration gate. Fresh
// databases are left for bootstrap; every existing file must pass the
// sequential migration plan before sqlite.Init can AutoMigrate current models.
func MigratePathIfExists(path string, options Options) error {
	// void running on first install and in-memory test databases
	dataPath := strings.TrimPrefix(path, "file:")
	if queryIndex := strings.IndexByte(dataPath, '?'); queryIndex >= 0 {
		dataPath = dataPath[:queryIndex]
	}
	if dataPath == "" || strings.Contains(dataPath, ":memory:") {
		return nil
	}
	if _, err := os.Stat(dataPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	hasSettings, err := pathHasSettingsTable(path)
	if err != nil {
		return err
	}
	if !hasSettings {
		return nil
	}
	return MigratePath(path, options)
}

func pathHasSettingsTable(path string) (bool, error) {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	probe, err := gorm.Open(sqlite.Open(path + separator + "mode=ro&_query_only=1"))
	if err != nil {
		return false, fmt.Errorf("open migration identity preflight: %w", err)
	}
	sqlDB, err := probe.DB()
	if err != nil {
		return false, err
	}
	defer sqlDB.Close()
	return probe.Migrator().HasTable("settings"), nil
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
	if err := rejectFutureVersion("core schema", preflightCoreVersion, dbschema.CurrentCoreVersion); err != nil {
		return err
	}
	// Rule compatibility is checked on a read-only pre-image before any schema
	// transaction or destructive cutover. Projection never rewrites stored rules.
	if err := readOnlyRuleUpgradePreflight(path); err != nil {
		return err
	}
	db, err := gorm.Open(sqlite.Open(sqliteMigrationDSN(path)), &gorm.Config{Logger: gormlogger.Discard})
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
	if err := rejectFutureVersion("core schema", coreVersion, dbschema.CurrentCoreVersion); err != nil {
		return err
	}
	if dbVersion != preflightDBVersion || coreVersion != preflightCoreVersion {
		return errors.New("database version changed after the read-only migration preflight")
	}
	fmt.Println("Current version:", currentVersion, "\nDatabase version:", dbVersion, "\nCore schema version:", coreVersion)
	if currentVersion == dbVersion && coreVersion == dbschema.CurrentCoreVersion {
		if err := validateCurrentMigrationJournals(db); err != nil {
			return err
		}
		fmt.Println("Database is up to date, no need to migrate")
		return nil
	}
	if cmp, ok := versionpolicy.CompareVersions(coreVersionOrBaseline(coreVersion), "1.11"); ok && cmp >= 0 {
		if err := validateOperationsMigrationJournal(db); err != nil {
			return err
		}
	}
	if coreVersion == dbschema.CurrentCoreVersion {
		if err := validateCurrentMigrationJournals(db); err != nil {
			return err
		}
	}
	legacyConfig, err := readLegacyConfig(options.LegacyConfigPath, dbVersion == "")
	if err != nil {
		return err
	}

	coreComparison, coreComparable := versionpolicy.CompareVersions(coreVersionOrBaseline(coreVersion), dbschema.CurrentCoreVersion)
	if !coreComparable {
		return errors.New("core schema version is not migration-compatible")
	}
	pending := []coreJournalContract{}
	if coreComparison < 0 {
		for _, contract := range coreJournalContracts() {
			comparison, _ := versionpolicy.CompareVersions(coreVersionOrBaseline(coreVersion), contract.Target)
			if comparison < 0 {
				if err := ensureCoreMigrationJournal(db, contract); err != nil {
					return err
				}
				pending = append(pending, contract)
			}
		}
	}
	tx := db.Begin()
	if tx.Error != nil {
		return fmt.Errorf("begin migration: %w", tx.Error)
	}
	transactionClosed := false
	defer func() {
		if !transactionClosed {
			_ = tx.Rollback().Error
		}
	}()
	abortMigration := func(cause error) error {
		rollbackErr := tx.Rollback().Error
		transactionClosed = true
		state := "FAILED"
		if rollbackErr != nil {
			state = "RECOVERY_REQUIRED"
			cause = errors.Join(cause, fmt.Errorf("rollback failed migration: %w", rollbackErr))
		}
		for _, contract := range pending {
			if journalErr := recordCoreMigrationState(db, contract, state, "core_migration_failed"); journalErr != nil {
				cause = errors.Join(cause, fmt.Errorf("record failed core migration: %w", journalErr))
			}
		}
		return cause
	}

	if err := integrity.EnsureNoTLSForeignKeyParent(tx); err != nil {
		return abortMigration(err)
	}
	if err := integrity.VerifyForeignKeysBeforeMigration(tx, integrity.Options{
		RepairForeignKeyOrphans: options.RepairForeignKeyOrphans,
	}); err != nil {
		return abortMigration(err)
	}

	fmt.Println("Start migrating database...")

	if _, err = steps.RunPending(tx, dbVersion, legacyConfig); err != nil {
		return abortMigration(err)
	}
	reportFindings := options.ReportFindings
	if reportFindings == nil {
		reportFindings = func(findings []diagnostics.Finding) {
			for _, finding := range findings {
				fmt.Printf("Migration candidate: %s [%s] %s: %s\n", finding.Path, finding.Code, finding.MigrationOutcome, finding.Message)
			}
		}
	}
	if coreVersion, err = steps.RunCorePendingWithOptions(tx, coreVersion, steps.CoreOptions{ProjectRuntimeFiles: options.ProjectRuntimeFiles, ReportFindings: reportFindings}); err != nil {
		return abortMigration(err)
	}
	if err = upsertVersionSetting(tx, "coreSchemaVersion", coreVersion); err != nil {
		return abortMigration(fmt.Errorf("update core schema version: %w", err))
	}

	// Persist the new version. The settings row is created lazily in older
	// schemas, so use UPSERT semantics.
	if err = upsertVersionSetting(tx, "version", currentVersion); err != nil {
		return abortMigration(fmt.Errorf("update version: %w", err))
	}
	for _, contract := range pending {
		if err := recordCoreMigrationState(tx, contract, "APPLIED", ""); err != nil {
			return abortMigration(fmt.Errorf("finalize core migration journal: %w", err))
		}
	}
	err = tx.Commit().Error
	transactionClosed = true
	if err != nil {
		state, code := "RECOVERY_REQUIRED", "core_migration_commit_ambiguous"
		// Version and APPLIED journal are in the same SQLite transaction as every
		// semantic row. An independent read of the unchanged publication markers
		// plus still-RUNNING exact journal proves the commit was rolled back.
		if commitRollbackProven(path, preflightDBVersion, preflightCoreVersion, pending) {
			state, code = "FAILED", "core_migration_commit_rolled_back"
		}
		for _, contract := range pending {
			if journalErr := recordCoreMigrationState(db, contract, state, code); journalErr != nil {
				err = errors.Join(err, fmt.Errorf("record ambiguous core migration commit: %w", journalErr))
			}
		}
		return fmt.Errorf("commit migration: %w", err)
	}
	if err = validateCurrentMigrationJournals(db); err != nil {
		return err
	}
	if err = checkpointWAL(db); err != nil {
		fmt.Println("Warning: WAL checkpoint skipped:", err)
	}
	fmt.Println("Migration done!")
	return nil
}

func readOnlyRuleUpgradePreflight(path string) error {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	probe, err := gorm.Open(sqlite.Open(path + separator + "mode=ro&_query_only=1"))
	if err != nil {
		return fmt.Errorf("open rule upgrade preflight: %w", err)
	}
	sqlDB, err := probe.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if !probe.Migrator().HasTable("settings") {
		return nil
	}
	var value string
	if err := probe.Table("settings").Select("value").Where("key = ?", "config").Scan(&value).Error; err != nil {
		return err
	}
	if strings.TrimSpace(value) == "" {
		return nil
	}
	base, err := singboxconfig.PrepareBaseOptionsUpgrade([]byte(value))
	if err != nil {
		return err
	}
	result, err := singboxvalidation.PrepareRuleUpgrade(base.Candidate)
	if err != nil {
		return err
	}
	for _, finding := range result.Findings {
		fmt.Printf("Rule upgrade: %s [%s]: %s\n", finding.Path, finding.Code, finding.Message)
	}
	return nil
}

func readLegacyConfig(path string, required bool) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" || !required {
		return nil, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect explicit legacy config: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxLegacyConfigBytes {
		return nil, errors.New("explicit legacy config is not a bounded regular file")
	}
	data, err := os.ReadFile(path) // #nosec G304 -- explicit operator-selected migration input.
	if err != nil {
		return nil, err
	}
	return data, nil
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

type coreJournalContract struct{ ID, Checksum, Target string }

func coreJournalContracts() []coreJournalContract {
	return []coreJournalContract{
		{steps.OperationsLifecycleStepID, steps.OperationsLifecycleChecksum, "1.11"},
		{steps.SingBoxStateStepID, steps.SingBoxStateChecksum, dbschema.CurrentCoreVersion},
	}
}
func ensureOperationsMigrationJournal(db *gorm.DB) error {
	return ensureCoreMigrationJournal(db, coreJournalContracts()[0])
}
func recordOperationsMigrationState(db *gorm.DB, state, errorCode string) error {
	return recordCoreMigrationState(db, coreJournalContracts()[0], state, errorCode)
}

func ensureCoreMigrationJournal(db *gorm.DB, contract coreJournalContract) error {
	if err := ensureOperationsMigrationJournalTable(db); err != nil {
		return fmt.Errorf("create core migration journal: %w", err)
	}
	var existing struct {
		Checksum string
		State    string
	}
	err := db.Raw("SELECT checksum, state FROM migration_journal_v1 WHERE scope = ? AND owner_id = ? AND step_id = ?",
		"core", "core", contract.ID).Row().Scan(&existing.Checksum, &existing.State)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if existing.Checksum != contract.Checksum {
			now := time.Now().Unix()
			if updateErr := db.Exec("UPDATE migration_journal_v1 SET state='RECOVERY_REQUIRED', compatibility_state='CHECKSUM_MISMATCH', error_code='core_migration_checksum_mismatch', finished_at=?, updated_at=? WHERE scope=? AND owner_id=? AND step_id=?",
				now, now, "core", "core", contract.ID).Error; updateErr != nil {
				return errors.Join(errors.New("core migration checksum changed; recovery is required"), updateErr)
			}
			return errors.New("core migration checksum changed; recovery is required")
		}
		if existing.State == "RECOVERY_REQUIRED" || existing.State == "APPLIED" {
			return fmt.Errorf("core migration journal state %s is inconsistent before schema %s", existing.State, contract.Target)
		}
	}
	return recordCoreMigrationState(db, contract, "RUNNING", "")
}

func recordCoreMigrationState(db *gorm.DB, contract coreJournalContract, state, errorCode string) error {
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
		"core", "core", contract.ID, contract.Checksum,
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
	return db.Transaction(func(tx *gorm.DB) error {
		version, err := readVersionSetting(tx, "coreSchemaVersion")
		if err != nil {
			return err
		}
		if version == "" && seedFresh {
			if err := upsertVersionSetting(tx, "coreSchemaVersion", dbschema.CurrentCoreVersion); err != nil {
				return err
			}
			version = dbschema.CurrentCoreVersion
		}
		if version != dbschema.CurrentCoreVersion {
			return fmt.Errorf("cannot seed current migration journal for core schema %q", version)
		}
		if err := ensureOperationsMigrationJournalTable(tx); err != nil {
			return err
		}
		for _, contract := range coreJournalContracts() {
			var count int64
			if err := tx.Raw("SELECT COUNT(*) FROM migration_journal_v1 WHERE scope=? AND owner_id=? AND step_id=?", "core", "core", contract.ID).Scan(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				if !seedFresh {
					return errors.New("current core schema migration journal is absent")
				}
				if err := recordCoreMigrationState(tx, contract, "APPLIED", ""); err != nil {
					return err
				}
			}
		}
		return validateCurrentMigrationJournals(tx)
	})
}

func validateOperationsMigrationJournal(db *gorm.DB) error {
	return validateCoreMigrationJournal(db, coreJournalContracts()[0])
}
func validateCurrentMigrationJournals(db *gorm.DB) error {
	for _, contract := range coreJournalContracts() {
		if err := validateCoreMigrationJournal(db, contract); err != nil {
			return err
		}
	}
	return nil
}
func validateCoreMigrationJournal(db *gorm.DB, contract coreJournalContract) error {
	if db == nil || !db.Migrator().HasTable("migration_journal_v1") {
		return errors.New("current core schema migration journal is absent")
	}
	var row struct {
		Checksum string
		State    string
	}
	err := db.Raw("SELECT checksum, state FROM migration_journal_v1 WHERE scope=? AND owner_id=? AND step_id=? LIMIT 1",
		"core", "core", contract.ID).Scan(&row).Error
	if err != nil {
		return err
	}
	if row.Checksum != contract.Checksum || row.State != "APPLIED" {
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

func commitRollbackProven(path, sourceVersion, sourceCore string, pending []coreJournalContract) bool {
	version, core, err := readOnlyVersionPreflight(path)
	if err != nil || version != sourceVersion || core != sourceCore {
		return false
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	probe, err := gorm.Open(sqlite.Open(path+separator+"mode=ro&_query_only=1"), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		return false
	}
	sqlDB, err := probe.DB()
	if err != nil {
		return false
	}
	defer sqlDB.Close()
	for _, contract := range pending {
		var row struct{ Checksum, State string }
		if err := probe.Raw("SELECT checksum,state FROM migration_journal_v1 WHERE scope='core' AND owner_id='core' AND step_id=?", contract.ID).Scan(&row).Error; err != nil || row.Checksum != contract.Checksum || row.State != "RUNNING" {
			return false
		}
	}
	return true
}
