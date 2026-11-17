package backup

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func manifestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "manifest.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Setting{Key: "coreSchemaVersion", Value: "1.13"}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func writeManifestFixture(t *testing.T, db *gorm.DB) BackupManifest {
	t.Helper()
	facts := ManifestFacts{AppVersion: "2026.5.1", InstalledOwners: func() ([]string, error) { return nil, nil }}
	if err := WriteManifest(context.Background(), db, []ManifestTable{{Name: "settings"}}, nil, facts); err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadAndVerifyManifest(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.AppVersion != "2026.5.1" || manifest.CoreSchema != "1.13" || manifest.BackupID == "" {
		t.Fatalf("source identity changed: %+v", manifest)
	}
	return manifest
}

func TestManifestRejectsChangedRowsAndExecutableOrUndeclaredInventory(t *testing.T) {
	for _, test := range []struct{ name, sql string }{
		{"changed-rows", "UPDATE settings SET value='different' WHERE key='coreSchemaVersion'"},
		{"undeclared-table", "CREATE TABLE undeclared (id INTEGER PRIMARY KEY)"},
		{"executable-trigger", "CREATE TRIGGER unexpected AFTER INSERT ON settings BEGIN UPDATE settings SET value='changed' WHERE key=NEW.key; END"},
		{"executable-view", "CREATE VIEW unexpected AS SELECT * FROM settings"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := manifestDatabase(t)
			writeManifestFixture(t, db)
			if err := db.Exec(test.sql).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := LoadAndVerifyManifest(context.Background(), db); err == nil {
				t.Fatal("altered archive was accepted")
			}
		})
	}
}

func TestManifestRejectsForgedOwnerAndUnknownJSONFields(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing-owner", true: "unknown-json-field"}[unknown], func(t *testing.T) {
			db := manifestDatabase(t)
			manifest := writeManifestFixture(t, db)
			if !unknown {
				manifest.Tables[0].Owner = "foreign-owner"
			}
			manifest.BackupID = DigestManifest(manifest)
			data, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if unknown {
				data = append(append([]byte{}, data[:len(data)-1]...), []byte(",\"untrusted\":true}")...)
			}
			if err := db.Model(&backupManifestRecord{}).Where("scope = ?", "backup").Updates(map[string]any{"payload": data, "digest": manifest.BackupID}).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := LoadAndVerifyManifest(context.Background(), db); err == nil {
				t.Fatal("forged archive was accepted")
			}
		})
	}
}

func TestManifestUnavailableInstalledOwnerFailsBeforePublication(t *testing.T) {
	for _, test := range []struct {
		name   string
		owners func() ([]string, error)
		reason string
	}{
		{"missing-catalog", nil, "inventory is unavailable"},
		{"failed-inventory", func() ([]string, error) { return nil, errors.New("fixture unavailable") }, "fixture unavailable"},
		{"missing-owner", func() ([]string, error) { return []string{"manifest-unavailable-owner"}, nil }, "manifest fails closed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := manifestDatabase(t)
			err := WriteManifest(context.Background(), db, []ManifestTable{{Name: "settings"}}, nil, ManifestFacts{AppVersion: "2026.5.1", InstalledOwners: test.owners})
			if err == nil || !strings.Contains(err.Error(), test.reason) {
				t.Fatalf("unexpected admission: %v", err)
			}
			if db.Migrator().HasTable(BackupManifestTable) {
				t.Fatal("rejected inventory published a manifest")
			}
			var version model.Setting
			if err := db.Where("key = ?", "coreSchemaVersion").First(&version).Error; err != nil || version.Value != "1.13" {
				t.Fatalf("rejection changed durable facts: %+v %v", version, err)
			}
		})
	}
}
