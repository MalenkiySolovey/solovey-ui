package entityinbounds_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/sessionidentity"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestManagedCredentialsRenderAndBindOnlyTheirAssignedOwner(t *testing.T) {
	for _, kind := range []string{"mieru", "trusttunnel"} {
		t.Run(kind, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
			if err != nil {
				t.Fatal(err)
			}
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			pool.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = pool.Close() })
			if err := db.AutoMigrate(&model.Client{}, &model.Inbound{}, &model.Setting{}, &model.AuditEvent{}); err != nil {
				t.Fatal(err)
			}
			captured, err := credentials.ApplyClientField(db, 42, kind, "", json.RawMessage(`{"intent":"replace","value":"synthetic-runtime-password"}`))
			if err != nil {
				t.Fatal(err)
			}
			credential, _ := json.Marshal(map[string]string{"name": "authenticated", "password": captured})
			config, _ := json.Marshal(map[string]json.RawMessage{kind: credential})
			inbound := model.Inbound{Id: 1, Type: kind, Tag: "native", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":23456}`)}
			if err := db.Create(&inbound).Error; err != nil {
				t.Fatal(err)
			}
			for _, row := range []model.Client{
				{Id: 42, Enable: true, Name: "different display name", Config: config, Inbounds: json.RawMessage(`[1]`)},
				{Id: 43, Enable: false, Config: config, Inbounds: json.RawMessage(`[1]`)},
				{Id: 44, Enable: true, Config: config, Inbounds: json.RawMessage(`[]`)},
			} {
				if err := db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}
			raw, err := inbound.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			rendered, err := (entityinbounds.StoredUsers{}).AddUsers(db, raw, 1, kind)
			if err != nil || bytes.Contains(rendered, []byte(captured)) {
				t.Fatal("scoped runtime render failed or retained an envelope")
			}
			var runtime struct {
				Users []struct{ Name, Password string }
			}
			if json.Unmarshal(rendered, &runtime) != nil || len(runtime.Users) != 1 || runtime.Users[0].Name != "authenticated" || runtime.Users[0].Password != "synthetic-runtime-password" {
				t.Fatal("runtime render did not use the enabled assigned owner's value")
			}
			bindings, err := sessionidentity.Capture(db, rendered)
			if err != nil || len(bindings) != 1 || bindings[0].ClientID != 42 || bindings[0].Principal != "authenticated" {
				t.Fatal("scoped credential lost its authenticated client binding")
			}
			encoded, _ := json.Marshal(bindings)
			if bytes.Contains(encoded, []byte("synthetic-runtime-password")) || bytes.Contains(encoded, []byte(captured)) {
				t.Fatal("runtime identity retained secret material")
			}
			rotated, err := credentials.ApplyClientField(db, 42, kind, captured, json.RawMessage(`{"intent":"replace","value":"synthetic-rotated-password"}`))
			if err != nil {
				t.Fatal(err)
			}
			credential, _ = json.Marshal(map[string]string{"name": "authenticated", "password": rotated})
			config, _ = json.Marshal(map[string]json.RawMessage{kind: credential})
			if err := db.Model(&model.Client{}).Where("id = ?", 42).Update("config", config).Error; err != nil {
				t.Fatal(err)
			}
			bindings, err = sessionidentity.Capture(db, rendered)
			if err != nil || len(bindings) != 0 {
				t.Fatal("stale runtime credential associated by principal alone")
			}
			if err := db.Exec(`CREATE TRIGGER deny_secret_use BEFORE INSERT ON audit_events WHEN NEW.event = 'owner_secret_use' BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`).Error; err != nil {
				t.Fatal(err)
			}
			if value, err := (entityinbounds.StoredUsers{}).AddUsers(db, raw, 1, kind); err == nil || len(value) != 0 {
				t.Fatal("runtime render bypassed synchronous audit admission")
			}
			if value, err := sessionidentity.Capture(db, rendered); err == nil || len(value) != 0 {
				t.Fatal("session binding bypassed synchronous audit admission")
			}
			if err := db.Exec(`DROP TRIGGER deny_secret_use`).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.Client{}).Where("id = ?", 42).Update("enable", false).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.Client{}).Where("id = ?", 43).Update("enable", true).Error; err != nil {
				t.Fatal(err)
			}
			if value, err := (entityinbounds.StoredUsers{}).AddUsers(db, raw, 1, kind); err == nil || len(value) != 0 {
				t.Fatal("copied envelope crossed its client identity")
			}
		})
	}
}
