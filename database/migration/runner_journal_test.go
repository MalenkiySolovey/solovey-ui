package migration

import (
	"path/filepath"
	"strings"
	"testing"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/database/migration/steps"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOperationsMigrationJournalResumesMatchingStepAndRejectsChecksumDrift(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:operations-migration-journal?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureOperationsMigrationJournal(db); err != nil {
		t.Fatal(err)
	}
	if err := ensureOperationsMigrationJournal(db); err != nil {
		t.Fatalf("matching interrupted step did not resume: %v", err)
	}
	var retry uint32
	if err := db.Raw("SELECT retry_count FROM migration_journal_v1 WHERE scope='core' AND owner_id='core' AND step_id=?",
		steps.OperationsLifecycleStepID).Scan(&retry).Error; err != nil || retry != 1 {
		t.Fatalf("retry_count=%d err=%v", retry, err)
	}
	if err := db.Exec("UPDATE migration_journal_v1 SET checksum=?, state='FAILED' WHERE scope='core' AND owner_id='core' AND step_id=?",
		strings.Repeat("0", 64), steps.OperationsLifecycleStepID).Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureOperationsMigrationJournal(db); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("checksum drift was accepted: %v", err)
	}
	var state, checksum string
	if err := db.Raw("SELECT state, checksum FROM migration_journal_v1 WHERE scope='core' AND owner_id='core' AND step_id=?",
		steps.OperationsLifecycleStepID).Row().Scan(&state, &checksum); err != nil {
		t.Fatal(err)
	}
	if state != "RECOVERY_REQUIRED" || checksum != strings.Repeat("0", 64) {
		t.Fatalf("drift journal state=%q checksum=%q", state, checksum)
	}
}

func TestCurrentSchemaRequiresExactJournalAndFreshBootstrapSeedsIt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:current-journal?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := EnsureCurrentSchemaJournal(db, false); err == nil {
		t.Fatal("missing current schema version was silently adopted")
	}
	if err := EnsureCurrentSchemaJournal(db, true); err != nil {
		t.Fatal(err)
	}
	if err := validateOperationsMigrationJournal(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE migration_journal_v1 SET checksum=? WHERE scope='core' AND owner_id='core' AND step_id=?",
		strings.Repeat("f", 64), steps.OperationsLifecycleStepID).Error; err != nil {
		t.Fatal(err)
	}
	if err := validateOperationsMigrationJournal(db); err == nil {
		t.Fatal("current schema checksum drift was accepted")
	}
}

func TestCurrentDatabaseWithoutAppliedJournalFailsBeforeVersionMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-current-journal.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO settings(key,value) VALUES('version',?),('coreSchemaVersion','1.11')",
		configidentity.GetVersion()).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := MigratePath(path, Options{}); err == nil || !strings.Contains(err.Error(), "journal") {
		t.Fatalf("missing current journal err=%v", err)
	}
	db = openMigrationDBAtPath(t, path)
	var version string
	if err := db.Raw("SELECT value FROM settings WHERE key='version'").Scan(&version).Error; err != nil {
		t.Fatal(err)
	}
	if version != configidentity.GetVersion() {
		t.Fatalf("missing-journal preflight mutated version=%q", version)
	}
}
