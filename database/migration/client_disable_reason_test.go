package migration

import (
	"fmt"
	"path/filepath"
	"testing"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/database/migration/steps"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbschema "github.com/MalenkiySolovey/solovey-ui/database/schema"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Minimal accepted 1.12 owner shape, intentionally without the new column.
// It is a migration contract fixture, not a historical database capture.
func clientDisableReasonFixture(t *testing.T) (string, *gorm.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "client-access.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	for _, statement := range []string{
		"CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)",
		"CREATE TABLE clients (id INTEGER PRIMARY KEY, enable NUMERIC, name TEXT, sub_secret TEXT, config BLOB, inbounds BLOB, volume INTEGER, expiry INTEGER, up INTEGER, down INTEGER)",
		`INSERT INTO clients VALUES (1,0,'legacy','retained-secret',CAST('{"trojan":{"password":"fixture"}}' AS BLOB),CAST('[7]' AS BLOB),100,0,101,5),(2,1,'enabled','retained-enabled',CAST('{}' AS BLOB),CAST('[]' AS BLOB),0,0,11,12),(3,NULL,'ambiguous-null','retained-null',CAST('{}' AS BLOB),CAST('[]' AS BLOB),0,0,0,0)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("INSERT INTO settings VALUES ('version',?),('coreSchemaVersion','1.12')", configidentity.GetVersion()).Error; err != nil {
		t.Fatal(err)
	}
	for _, contract := range coreJournalContracts()[:2] {
		if err := ensureCoreMigrationJournal(db, contract); err != nil {
			t.Fatal(err)
		}
		if err := recordCoreMigrationState(db, contract, "APPLIED", ""); err != nil {
			t.Fatal(err)
		}
	}
	return path, db
}

func TestClientDisableMigrationConservativeAtomicAndRetryable(t *testing.T) {
	path, db := clientDisableReasonFixture(t)
	if err := db.Exec(fmt.Sprintf(`CREATE TRIGGER reject_access_version BEFORE UPDATE ON settings WHEN NEW.key='coreSchemaVersion' AND NEW.value='%s' BEGIN SELECT RAISE(ABORT,'injected client publication failure'); END`, dbschema.CurrentCoreVersion)).Error; err != nil {
		t.Fatal(err)
	}
	before := semanticPreimage(t, db, true)
	if err := MigratePath(path, Options{}); err == nil {
		t.Fatal("late publication failure was ignored")
	}
	if semanticPreimage(t, db, true) != before || db.Migrator().HasColumn(&model.Client{}, "AccessDisableReason") {
		t.Fatal("failed migration changed legacy schema or access rows")
	}
	var journalState string
	if err := db.Raw("SELECT state FROM migration_journal_v1 WHERE step_id=?", steps.ClientDisableReasonStepID).Scan(&journalState).Error; err != nil || journalState != "FAILED" {
		t.Fatalf("failed migration journal: %s, %v", journalState, err)
	}
	if err := db.Exec("DROP TRIGGER reject_access_version").Error; err != nil {
		t.Fatal(err)
	}
	if err := MigratePath(path, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := validateCurrentMigrationJournals(db); err != nil {
		t.Fatal(err)
	}
	var rows []model.Client
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Enable || rows[0].AccessDisableReason != model.ClientDisabledLegacy || !rows[1].Enable || rows[1].AccessDisableReason != "" || rows[2].AccessDisableReason != model.ClientDisabledLegacy {
		t.Fatalf("legacy access was guessed or enabled: %#v", rows)
	}
	if rows[0].SubSecret != "retained-secret" || string(rows[0].Config) != `{"trojan":{"password":"fixture"}}` || string(rows[0].Inbounds) != `[7]` || rows[0].Volume != 100 || rows[0].Up != 101 || rows[0].Down != 5 {
		t.Fatal("migration changed credentials, membership, limits or counters")
	}
	version, err := readVersionSetting(db, "coreSchemaVersion")
	if err != nil || version != dbschema.CurrentCoreVersion {
		t.Fatal("new schema was not atomically published")
	}
	after := semanticPreimage(t, db, false)
	if err := MigratePath(path, Options{}); err != nil {
		t.Fatal(err)
	}
	if semanticPreimage(t, db, false) != after {
		t.Fatal("current migration was not an exact no-op")
	}
}

func TestClientDisableMigrationChecksHistoricalJournalBeforeMutation(t *testing.T) {
	path, db := clientDisableReasonFixture(t)
	if err := db.Exec("UPDATE migration_journal_v1 SET checksum='wrong' WHERE step_id=?", steps.SingBoxStateStepID).Error; err != nil {
		t.Fatal(err)
	}
	before := semanticPreimage(t, db, false)
	if err := MigratePath(path, Options{}); err == nil {
		t.Fatal("schema 1.12 journal drift was accepted while advancing to 1.13")
	}
	if semanticPreimage(t, db, false) != before {
		t.Fatal("journal preflight mutated legacy data")
	}
}

func TestClientDisableReasonSchemaContractIsReviewed(t *testing.T) {
	const reviewed = "1fb72840a321644eafa8a743ebea53d7e9cbf345609bd08499d2af4985fe4edd"
	if steps.ClientDisableReasonChecksum != reviewed {
		t.Fatal("Client disable migration contract changed without review")
	}
}
