package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDNSServiceReferenceGuardsBeforePersistence(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Service{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	row := model.Service{Type: "resolved", Tag: "resolver", Options: json.RawMessage(`{}`)}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Setting{Key: "config", Value: `{"dns":{"servers":[{"type":"resolved","service":"resolver"}]}}`}).Error; err != nil {
		t.Fatal(err)
	}
	for _, operation := range []func() error{
		func() error { _, err := saveDelete(db, json.RawMessage(`"resolver"`)); return err },
		func() error {
			_, err := saveUpsert(db, "edit", json.RawMessage(fmt.Sprintf(`{"id":%d,"type":"resolved","tag":"renamed"}`, row.Id)))
			return err
		},
		func() error {
			_, err := saveUpsert(db, "edit", json.RawMessage(fmt.Sprintf(`{"id":%d,"type":"oom-killer","tag":"resolver"}`, row.Id)))
			return err
		},
	} {
		if err := operation(); err == nil || !strings.Contains(err.Error(), "dns.servers[0].service") {
			t.Fatal("referenced service mutation accepted or lost locator")
		}
		var persisted model.Service
		if err := db.First(&persisted, row.Id).Error; err != nil || persisted.Tag != "resolver" {
			t.Fatal("rejected mutation changed source")
		}
	}
	if err := db.Where("key = ?", "config").Delete(&model.Setting{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := saveUpsert(db, "edit", json.RawMessage(fmt.Sprintf(`{"id":%d,"type":"resolved","tag":"resolver","listen_port":"invalid"}`, row.Id))); err == nil {
		t.Fatal("malformed resolved service persisted")
	}
	change, err := saveUpsert(db, "edit", json.RawMessage(fmt.Sprintf(`{"id":%d,"type":"resolved","tag":"resolver","listen_port":5354}`, row.Id)))
	if err != nil || !change.NeedsRestart {
		t.Fatal("resolved replacement did not request complete core lifecycle")
	}
	change, err = saveDelete(db, json.RawMessage(`"resolver"`))
	if err != nil || !change.NeedsRestart {
		t.Fatal("resolved delete did not request complete core lifecycle")
	}
}
