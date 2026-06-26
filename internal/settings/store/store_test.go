package store

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newSettingsStoreTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "CGO_ENABLED=0") {
			t.Skip("sqlite driver requires CGO in this environment")
		}
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatalf("migrate settings: %v", err)
	}
	return db
}

func TestInsertIfMissing(t *testing.T) {
	db := newSettingsStoreTestDB(t)
	if err := InsertIfMissing(db, "k", "first"); err != nil {
		t.Fatalf("InsertIfMissing first: %v", err)
	}
	if err := InsertIfMissing(db, "k", "second"); err != nil {
		t.Fatalf("InsertIfMissing second: %v", err)
	}
	var setting model.Setting
	if err := db.Where("key = ?", "k").First(&setting).Error; err != nil {
		t.Fatalf("read setting: %v", err)
	}
	if setting.Value != "first" {
		t.Fatalf("value = %q, want first", setting.Value)
	}
}

func TestUpsertValue(t *testing.T) {
	db := newSettingsStoreTestDB(t)
	if err := UpsertValue(db, "k", "first"); err != nil {
		t.Fatalf("UpsertValue insert: %v", err)
	}
	if err := UpsertValue(db, "k", "second"); err != nil {
		t.Fatalf("UpsertValue update: %v", err)
	}
	var setting model.Setting
	if err := db.Where("key = ?", "k").First(&setting).Error; err != nil {
		t.Fatalf("read setting: %v", err)
	}
	if setting.Value != "second" {
		t.Fatalf("value = %q, want second", setting.Value)
	}
}
