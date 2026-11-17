package migration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func fixtureDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handle.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Exec("CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO settings(key,value) VALUES ('version','2026.4.1'),('coreSchemaVersion','1.12')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE migration_probe (value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func fixturePlan() Plan {
	return Plan{
		ProductVersion: "2026.5.1", CoreVersion: "1.13",
		CompareVersions: func(left, right string) (int, bool) {
			order := map[string]int{"2026.4.1": 0, "2026.5.1": 1, "1.12": 2, "1.13": 3}
			l, leftOK := order[left]
			r, rightOK := order[right]
			if !leftOK || !rightOK {
				return 0, false
			}
			if l < r {
				return -1, true
			}
			if l > r {
				return 1, true
			}
			return 0, true
		},
		Journal:             []JournalContract{{ID: "client-disable-reason", Checksum: "accepted-client-schema", Target: "1.13"}},
		ValidateStoredState: func(*gorm.DB) error { return nil },
		VerifyIntegrity:     func(*gorm.DB) error { return nil },
		MigrateLegacy:       func(_ *gorm.DB, version string, _ []byte) (string, error) { return version, nil },
		MigrateCore:         func(*gorm.DB, string) (string, error) { return "1.13", nil },
	}
}

func assertVersion(t *testing.T, db *gorm.DB, key, want string) {
	t.Helper()
	value, err := ReadVersionSetting(db, key)
	if err != nil || value != want {
		t.Fatalf("%s = %q, %v; want %q", key, value, err, want)
	}
}

func TestIncompleteMigrationPlanRejectsBeforeCreatingCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.db")
	if err := MigratePath(path, Plan{}, Options{}); err == nil || !strings.Contains(err.Error(), "plan is incomplete") {
		t.Fatalf("incomplete plan = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete plan opened writable candidate: %v", err)
	}
}

func TestStoredStatePreflightCannotWriteCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.db")
	db := fixtureDB(t, path)
	plan := fixturePlan()
	called := false
	plan.ValidateStoredState = func(candidate *gorm.DB) error {
		return candidate.Exec("UPDATE settings SET value='mutated' WHERE key='version'").Error
	}
	plan.VerifyIntegrity = func(*gorm.DB) error { called = true; return nil }
	if err := MigratePath(path, plan, Options{}); err == nil {
		t.Fatal("read-only preflight wrote candidate")
	}
	if called || db.Migrator().HasTable("migration_journal_v1") {
		t.Fatal("rejected preflight entered migration authority")
	}
	assertVersion(t, db, "version", "2026.4.1")
}

func TestMigrationFailureRollsBackSharedTransactionAndRetryPublishesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.db")
	db := fixtureDB(t, path)
	plan := fixturePlan()
	insert := func(tx *gorm.DB, value string) error {
		return tx.Exec("INSERT INTO migration_probe(value) VALUES(?)", value).Error
	}
	count := func(tx *gorm.DB, want int64) error {
		var got int64
		if err := tx.Table("migration_probe").Count(&got).Error; err != nil {
			return err
		}
		if got != want {
			t.Fatalf("migration steps did not share one transaction: %d, want %d", got, want)
		}
		return nil
	}
	calls := 0
	plan.VerifyIntegrity = func(tx *gorm.DB) error { calls++; return insert(tx, "integrity") }
	plan.MigrateLegacy = func(tx *gorm.DB, version string, _ []byte) (string, error) {
		calls++
		if err := count(tx, 1); err != nil {
			return "", err
		}
		return version, insert(tx, "legacy")
	}
	rejected := errors.New("core owner rejected candidate")
	fail := true
	plan.MigrateCore = func(tx *gorm.DB, _ string) (string, error) {
		calls++
		if err := count(tx, 2); err != nil {
			return "", err
		}
		if err := insert(tx, "core"); err != nil {
			return "", err
		}
		if fail {
			return "", rejected
		}
		return "1.13", nil
	}
	if err := MigratePath(path, plan, Options{}); !errors.Is(err, rejected) {
		t.Fatalf("owner rejection = %v", err)
	}
	if err := count(db, 0); err != nil {
		t.Fatal(err)
	}
	assertVersion(t, db, "version", "2026.4.1")
	assertVersion(t, db, "coreSchemaVersion", "1.12")
	var state string
	if err := db.Raw("SELECT state FROM migration_journal_v1").Scan(&state).Error; err != nil || state != "FAILED" {
		t.Fatalf("failed journal = %q, %v", state, err)
	}
	fail = false
	if err := MigratePath(path, plan, Options{}); err != nil {
		t.Fatal(err)
	}
	if err := count(db, 3); err != nil {
		t.Fatal(err)
	}
	assertVersion(t, db, "version", "2026.5.1")
	assertVersion(t, db, "coreSchemaVersion", "1.13")
	if err := ValidateCurrentMigrationJournals(db, plan.Journal); err != nil {
		t.Fatal(err)
	}
	var retries int
	if err := db.Raw("SELECT retry_count FROM migration_journal_v1").Scan(&retries).Error; err != nil || retries != 1 {
		t.Fatalf("retry journal = %d, %v", retries, err)
	}
	if calls != 6 {
		t.Fatalf("ordered migration calls = %d", calls)
	}
	calls = 0
	if err := MigratePath(path, plan, Options{}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("current schema replayed owner mutations")
	}
}

func TestMigrationCandidateNeverChangesSeparateLiveDatabase(t *testing.T) {
	root := t.TempDir()
	live := fixtureDB(t, filepath.Join(root, "live.db"))
	path := filepath.Join(root, "candidate.db")
	candidate := fixtureDB(t, path)
	if err := MigratePath(path, fixturePlan(), Options{}); err != nil {
		t.Fatal(err)
	}
	assertVersion(t, candidate, "coreSchemaVersion", "1.13")
	assertVersion(t, live, "version", "2026.4.1")
	assertVersion(t, live, "coreSchemaVersion", "1.12")
	if live.Migrator().HasTable("migration_journal_v1") {
		t.Fatal("candidate migration journal appeared on live database")
	}
}

func TestMigrationCannotPublishAnUnfinishedCorePlan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.db")
	db := fixtureDB(t, path)
	plan := fixturePlan()
	plan.MigrateCore = func(*gorm.DB, string) (string, error) { return "1.12", nil }
	if err := MigratePath(path, plan, Options{}); err == nil || !strings.Contains(err.Error(), "supported sequential migration plan") {
		t.Fatalf("unfinished plan = %v", err)
	}
	assertVersion(t, db, "version", "2026.4.1")
	assertVersion(t, db, "coreSchemaVersion", "1.12")
}
