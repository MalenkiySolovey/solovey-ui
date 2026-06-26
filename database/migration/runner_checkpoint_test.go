package migration

import (
	"path/filepath"
	"strings"
	"testing"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrateDbFrom14RunsCheckpointAfterCommit(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dbDir)
	dbPath := filepath.Join(dbDir, configidentity.GetName()+".db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
CREATE TABLE settings (
	id integer PRIMARY KEY AUTOINCREMENT,
	key text,
	value text
)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO settings(key, value) VALUES('version', '1.4.3')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
CREATE TABLE clients (
	id integer PRIMARY KEY AUTOINCREMENT,
	enable boolean,
	name text
)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO clients(enable, name) VALUES(1, 'alice')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
CREATE TABLE audit_events (
	id integer PRIMARY KEY AUTOINCREMENT,
	date_time integer,
	actor text,
	event text,
	resource text,
	severity text,
	ip text,
	user_agent text,
	details blob
)`).Error; err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		if err := sqlDB.Close(); err != nil {
			t.Fatal(err)
		}
	}

	if err := MigrateDb(); err != nil {
		t.Fatal(err)
	}

	db = openMigrationDBAtPath(t, dbPath)
	var version string
	if err := db.Raw("SELECT value FROM settings WHERE key = ?", "version").Scan(&version).Error; err != nil {
		t.Fatal(err)
	}
	if version != configidentity.GetVersion() {
		t.Fatalf("version was not updated: got %q want %q", version, configidentity.GetVersion())
	}
	var subSecret string
	if err := db.Raw("SELECT sub_secret FROM clients WHERE name = ?", "alice").Scan(&subSecret).Error; err != nil {
		t.Fatal(err)
	}
	if subSecret == "" {
		t.Fatal("sub_secret was not backfilled")
	}
}

func openMigrationDBAtPath(t *testing.T, dbPath string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}
