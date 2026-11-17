package backup

import (
	"context"
	"encoding/json"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type typedBackupZeroValues struct {
	ID         uint   `gorm:"primaryKey"`
	Enabled    bool   `gorm:"default:true"`
	Count      int    `gorm:"default:7"`
	Label      string `gorm:"default:new"`
	CreatedAt  int64  `gorm:"autoCreateTime"`
	UpdatedAt  int64  `gorm:"autoUpdateTime"`
	Payload    json.RawMessage
	Attributes map[string]string `gorm:"serializer:json"`
	Optional   *int
}

func TestTypedBackupPreservesStoredZeroValues(t *testing.T) {
	open := func(name string) *gorm.DB {
		t.Helper()
		db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), name+".db")), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		if err := db.AutoMigrate(&typedBackupZeroValues{}); err != nil {
			t.Fatal(err)
		}
		return db
	}
	source, destination := open("source"), open("backup")
	// SQL establishes stored facts independent of model creation defaults.
	if err := source.Exec(`INSERT INTO typed_backup_zero_values
		(id, enabled, count, label, created_at, updated_at, payload, attributes, optional)
		VALUES (1, 0, 0, '', 0, 0, ?, ?, NULL), (2, 1, 9, 'kept', 123, 456, ?, ?, 8)`,
		[]byte(`{"zero":0}`), `{"owner":"disabled"}`, []byte(`{"one":1}`), `{"owner":"enabled"}`).Error; err != nil {
		t.Fatal(err)
	}
	var before []typedBackupZeroValues
	if err := source.Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := copyBackupTable(source, destination, &typedBackupZeroValues{}); err != nil {
		t.Fatal(err)
	}
	var after []typedBackupZeroValues
	if err := destination.Order("id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("logical backup changed stored values:\nbefore=%+v\nafter=%+v", before, after)
	}
}

func TestCopyOpaqueTableRejectsExecutableTriggers(t *testing.T) {
	source, err := gorm.Open(sqlite.Open("file:opaque-trigger-source?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	destination, err := gorm.Open(sqlite.Open("file:opaque-trigger-destination?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Exec("CREATE TABLE opaque_rows (id INTEGER PRIMARY KEY, value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := source.Exec("CREATE TRIGGER opaque_rows_trigger AFTER INSERT ON opaque_rows BEGIN UPDATE opaque_rows SET value='changed' WHERE id=NEW.id; END").Error; err != nil {
		t.Fatal(err)
	}
	if err := copyOpaqueTable(context.Background(), source, destination, "opaque_rows"); err == nil || !strings.Contains(err.Error(), "triggers") {
		t.Fatalf("opaque trigger was accepted: %v", err)
	}
}
