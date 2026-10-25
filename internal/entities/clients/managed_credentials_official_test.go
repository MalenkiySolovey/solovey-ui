//go:build !shtorm_extended

package entityclients

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
)

func TestOfficialRejectsNewManagedCredentialsBeforeIdentityOrKeyMutation(t *testing.T) {
	for _, protocol := range []string{"mieru", "trusttunnel", "ssh", "mtproxy"} {
		t.Run(protocol, func(t *testing.T) {
			db := newClientDB(t)
			field := "password"
			if protocol == "mtproxy" {
				field = "secret"
			}
			credential, _ := json.Marshal(map[string]any{"name": "alice", field: map[string]any{"intent": "replace", "value": "synthetic-private-value"}})
			config, _ := json.Marshal(map[string]json.RawMessage{protocol: credential})
			client := model.Client{Name: "alice", Enable: true, Config: config, Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)}
			for _, action := range []string{"new", "addbulk"} {
				var data []byte
				if action == "new" {
					data, _ = json.Marshal(client)
				} else {
					data, _ = json.Marshal([]model.Client{client})
				}
				_, err := Save(SaveRequest{Tx: db, Action: action, Data: data})
				if err == nil || !strings.Contains(err.Error(), "CLIENT_CREDENTIAL_CAPABILITY_UNAVAILABLE") || strings.Contains(err.Error(), "synthetic-private-value") {
					t.Fatal("unavailable credential accepted or exposed")
				}
				var count int64
				if err := db.Model(&model.Client{}).Count(&count).Error; err != nil || count != 0 {
					t.Fatal("failed save left reserved client identity")
				}
				if db.Migrator().HasTable(&model.Setting{}) || db.Migrator().HasTable(&model.AuditEvent{}) {
					t.Fatal("unavailable save created secret state")
				}
			}
		})
	}
}

func TestOfficialPreservesUnavailableCredentialThroughMetadataReadAndEdit(t *testing.T) {
	db := newClientDB(t)
	if err := db.AutoMigrate(&model.Setting{}, &model.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	captured, err := credentials.ApplyClientField(db, 42, "mieru", "", json.RawMessage(`{"intent":"replace","value":"synthetic-private-value"}`))
	if err != nil {
		t.Fatal(err)
	}
	credential, _ := json.Marshal(map[string]any{"name": "alice", "password": captured})
	config, _ := json.Marshal(map[string]json.RawMessage{"mieru": credential})
	stored := model.Client{Id: 42, Name: "alice", Enable: true, Config: config, Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), SubSecret: "synthetic-subscription-id"}
	if err := db.Create(&stored).Error; err != nil {
		t.Fatal(err)
	}
	projected, err := GetByID(db, "42")
	if err != nil || len(*projected) != 1 || bytes.Contains((*projected)[0].Config, []byte(captured)) || bytes.Contains((*projected)[0].Config, []byte("synthetic-private-value")) {
		t.Fatal("normal client read disclosed credential material")
	}
	var view struct {
		Mieru struct{ Password secretref.View }
	}
	if json.Unmarshal((*projected)[0].Config, &view) != nil || !view.Mieru.Password.Present {
		t.Fatal("missing scoped reference")
	}
	mutation := map[string]any{"intent": "unchanged", "reference": view.Mieru.Password.Reference, "present": true, "rotationRevision": view.Mieru.Password.RotationRevision}
	credential, _ = json.Marshal(map[string]any{"name": "alice", "password": mutation})
	config, _ = json.Marshal(map[string]json.RawMessage{"mieru": credential})
	edited := stored
	edited.Desc, edited.Config = "description changed", config
	data, _ := json.Marshal(edited)
	if _, err := Save(SaveRequest{Tx: db, Action: "edit", Data: data}); err != nil {
		t.Fatal("preservable metadata edit denied", err)
	}
	var actual model.Client
	if err := db.First(&actual, 42).Error; err != nil || actual.Desc != edited.Desc || !bytes.Contains(actual.Config, []byte(captured)) {
		t.Fatal("credential changed or metadata edit lost")
	}
	edited.Config, edited.Desc = json.RawMessage(`{}`), "older editor omitted managed credentials"
	data, _ = json.Marshal(edited)
	if _, err := Save(SaveRequest{Tx: db, Action: "edit", Data: data}); err != nil {
		t.Fatal("older metadata editor lost preservation", err)
	}
	if err := db.First(&actual, 42).Error; err != nil || actual.Desc != edited.Desc || !bytes.Contains(actual.Config, []byte(captured)) {
		t.Fatal("omitted unavailable credential was lost")
	}
	before := append([]byte(nil), actual.Config...)
	credential, _ = json.Marshal(map[string]any{"name": "alice", "password": map[string]any{"intent": "clear"}})
	edited.Config, _ = json.Marshal(map[string]json.RawMessage{"mieru": credential})
	data, _ = json.Marshal(edited)
	if _, err := Save(SaveRequest{Tx: db, Action: "edit", Data: data}); err == nil {
		t.Fatal("Official changed an unavailable credential")
	}
	if err := db.First(&actual, 42).Error; err != nil || !bytes.Equal(actual.Config, before) {
		t.Fatal("failed unsupported edit lost stored credential")
	}
}
