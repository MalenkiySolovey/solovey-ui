package steps

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNormalizeDNSWithoutConfigRowIsNoop(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}, &model.Client{}, &model.Outbound{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Setting{Key: "version", Value: "1.2"}).Error; err != nil {
		t.Fatal(err)
	}

	if err := normalizeDNSAndOutboundOptions(db); err != nil {
		t.Fatalf("config-less 1.2 to 1.3 migration failed: %v", err)
	}
}
