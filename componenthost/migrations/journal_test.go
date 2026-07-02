package migrations

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRecordAppliedUpsertsComponentMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "components.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := EnsureJournal(db); err != nil {
		t.Fatal(err)
	}

	item := manifest.Manifest{
		ID:       "telegram",
		Name:     "Telegram",
		Version:  "1",
		Delivery: manifest.DeliveryInProcess,
	}
	if err := RecordApplied(db, item); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	item.Name = "Telegram Updated"
	if err := RecordApplied(db, item); err != nil {
		t.Fatal(err)
	}

	var records []model.ComponentMigration
	if err := db.Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one upserted migration record, got %#v", records)
	}
	if records[0].Name != "Telegram Updated" {
		t.Fatalf("journal did not update metadata: %#v", records[0])
	}
}
