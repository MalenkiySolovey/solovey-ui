package outbounds

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEnsureDefaultFillsEmptySchemaIdempotently(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:outbound-default?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Outbound{}, &model.Endpoint{}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDefault(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDefault(db); err != nil {
		t.Fatal(err)
	}
	var rows []model.Outbound
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Type != "direct" || rows[0].Tag != DirectTag || rows[0].SortOrder != 1 {
		t.Fatalf("default outbounds = %#v", rows)
	}
}

func TestEnsureDefaultHonorsSharedEndpointNamespace(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:outbound-default-conflict?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Outbound{}, &model.Endpoint{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Endpoint{Type: "wireguard", Tag: DirectTag, Options: []byte("{}")}).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnsureDefault(db); err == nil {
		t.Fatal("default outbound ignored endpoint namespace conflict")
	}
}
