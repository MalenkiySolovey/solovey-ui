package identity

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestValidateTypeTagRejectsInvalidPersistentIdentity(t *testing.T) {
	for _, values := range [][2]string{{"", "tag"}, {"direct", ""}, {"direct", " padded "}, {"direct", "bad\n"}} {
		if err := ValidateTypeTag(values[0], values[1]); err == nil {
			t.Fatalf("invalid type/tag was accepted: %#v", values)
		}
	}
	if err := ValidateTypeTag("direct", "valid-tag"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateStoredRejectsDuplicateClientNames(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:stored-client-identity?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Client{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := db.Create(&model.Client{Name: "duplicate", Inbounds: []byte("[]"), Links: []byte("[]")}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := ValidateStored(db); err == nil || !strings.Contains(err.Error(), "duplicates name") {
		t.Fatalf("ValidateStored() error = %v, want duplicate client name", err)
	}
}

func TestEnsureOutboundTagAvailableRejectsCrossTableDuplicate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:entity-identity?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Outbound{}, &model.Endpoint{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Endpoint{Type: "wireguard", Tag: "shared", Options: []byte("{}")}).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnsureOutboundTagAvailable(db, "shared", 0, 0); err == nil {
		t.Fatal("endpoint tag was available to an outbound")
	}
}

func TestValidateStoredRejectsInvalidAndCrossTableDuplicateIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:stored-entity-identity?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Inbound{}, &model.Outbound{}, &model.Endpoint{}, &model.Service{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Outbound{Type: "direct", Tag: "shared", Options: []byte("{}")}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Endpoint{Type: "wireguard", Tag: "shared", Options: []byte("{}")}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ValidateStored(db); err == nil || !strings.Contains(err.Error(), "duplicates tag") {
		t.Fatalf("ValidateStored() error = %v, want shared namespace duplicate", err)
	}
	if err := db.Where("tag = ?", "shared").Delete(&model.Endpoint{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO services(type, tag) VALUES(?, ?)", "dns", " bad ").Error; err != nil {
		t.Fatal(err)
	}
	if err := ValidateStored(db); err == nil || !strings.Contains(err.Error(), "entity tag is invalid") {
		t.Fatalf("ValidateStored() error = %v, want invalid stored tag", err)
	}
}
