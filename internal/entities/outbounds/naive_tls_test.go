package outbounds

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNaiveUnsupportedTLSCannotPersistCreateOrEdit(t *testing.T) {
	if !registry.Resolve("outbounds", "naive").Available() {
		t.Skip("Naive outbound is not compiled in this test profile")
	}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.Outbound{}, &model.Endpoint{}, &model.Service{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&model.Setting{Key: "coreSchemaVersion", Value: "1.12"}).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	valid := map[string]any{"type": "naive", "tag": "naive-fixture", "server": "127.0.0.1", "server_port": 8443, "tls": map[string]any{"enabled": true, "server_name": "fixture.invalid"}}
	raw, _ := json.Marshal(valid)
	if _, err = Save(db, "new", raw); err != nil {
		t.Fatal(err)
	}
	var before model.Outbound
	if err = db.Where("tag = ?", "naive-fixture").First(&before).Error; err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"new", "edit"} {
		t.Run(action, func(t *testing.T) {
			for _, field := range []string{"insecure", "certificate_public_key_sha256", "missing-tls"} {
				draft := map[string]any{}
				if err := json.Unmarshal(raw, &draft); err != nil {
					t.Fatal(err)
				}
				if action == "edit" {
					draft["id"] = before.Id
				} else {
					draft["tag"] = "rejected-" + field
				}
				tls := draft["tls"].(map[string]any)
				if field == "missing-tls" {
					delete(draft, "tls")
				} else if field == "insecure" {
					tls[field] = true
				} else {
					tls[field] = []string{"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}
				}
				invalid, _ := json.Marshal(draft)
				change, err := Save(db, action, invalid)
				if err == nil || change != nil {
					t.Fatalf("invalid %s yielded persistent/runtime mutation", action)
				}
				var after model.Outbound
				if err = db.Where("id = ?", before.Id).First(&after).Error; err != nil {
					t.Fatal(err)
				}
				var count int64
				if err = db.Model(&model.Outbound{}).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				if count != 1 || !bytes.Equal(after.Options, before.Options) || after.Tag != before.Tag {
					t.Fatal("unsupported TLS changed the valid stored row or created another row")
				}
			}
		})
	}
}
