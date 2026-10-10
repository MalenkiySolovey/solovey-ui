package sqlite

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDiagnosticReadOnlyKeepsLegacyDatabaseAndRuntimeOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy # &資料.db")
	writer, err := gorm.Open(gormsqlite.Open(path), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := writer.DB()
	if err := writer.Exec("CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := writer.Exec("INSERT INTO settings VALUES ('coreSchemaVersion', '1.8')").Error; err != nil {
		t.Fatal(err)
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := DB()
	reader, err := OpenReadOnly(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	readPool, _ := reader.DB()
	var version string
	if err := reader.Raw("SELECT value FROM settings WHERE key = 'coreSchemaVersion'").Scan(&version).Error; err != nil || version != "1.8" {
		t.Fatal("legacy marker cannot be read without adaptation", err)
	}
	if err := reader.Exec("UPDATE settings SET value = '1.12'").Error; err == nil {
		t.Fatal("diagnostic connection admitted a write")
	}
	if DB() != owner {
		t.Fatal("diagnostics replaced the runtime owner")
	}
	if err := readPool.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("diagnostics changed the database file", err)
	}
}

func TestDiagnosticReadOnlyDoesNotCreateMissingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent", "solovey-ui.db")
	if _, err := OpenReadOnly(context.Background(), path); err == nil {
		t.Fatal("absent database was accepted")
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("diagnostics created a directory", err)
	}
}
