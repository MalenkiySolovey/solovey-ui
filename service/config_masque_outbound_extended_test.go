//go:build shtorm_extended && with_quic

package service

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
)

func TestMASQUEProfileCredentialsRetainTheirNestedOwner(t *testing.T) {
	initSettingTestDB(t)
	seedConfigBlobForHotReloadTest(t, json.RawMessage(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`))
	svc, _, _ := newHotReloadConfigService(t)
	db := dbsqlite.DB()
	profile := map[string]any{"id": "fixture-device", "recreate": false}
	secrets := map[string]string{"auth_token": "private-masque-token-canary", "license_key": "private-masque-license-canary", "private_key": "private-masque-key-canary"}
	for field, value := range secrets {
		profile[field] = map[string]string{"intent": "replace", "value": value}
	}
	draft := map[string]any{"type": "masque", "tag": "owned-masque", "profile": profile, "use_l4_proxy": true, "tls": map[string]any{"server_name": "fixture.invalid"}}
	save := func(action string, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.Save("outbounds", action, raw, "", "admin", "fixture.invalid")
		return err
	}
	if err := save("new", draft); err != nil {
		t.Fatal(err)
	}
	var stored model.Outbound
	if err := db.Where("tag = ?", "owned-masque").First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	for _, value := range secrets {
		if bytes.Contains(stored.Options, []byte(value)) {
			t.Fatal("profile credential was stored as plaintext")
		}
	}
	view, err := entityoutbounds.GetAll(db)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(view)
	if bytes.Contains(encoded, []byte("canary")) || bytes.Contains(encoded, []byte("sbox:v1:")) {
		t.Fatal("normal read disclosed a profile credential")
	}
	var rows []map[string]any
	if json.Unmarshal(encoded, &rows) != nil {
		t.Fatal("invalid normal read")
	}
	var editable map[string]any
	for _, row := range rows {
		if row["tag"] == "owned-masque" {
			editable = row
		}
	}
	if editable == nil {
		t.Fatal("profile row missing")
	}
	nested := editable["profile"].(map[string]any)
	for field := range secrets {
		ref := nested[field].(map[string]any)
		if ref["present"] != true {
			t.Fatal("missing nested masked reference")
		}
		nested[field] = map[string]any{"intent": "unchanged", "reference": ref["reference"], "present": ref["present"], "rotationRevision": ref["rotationRevision"]}
	}
	nested["id"] = "edited-device"
	if err := save("edit", editable); err != nil {
		t.Fatal("masked nested edit rejected", err)
	}
	config, err := svc.GetConfig("")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range secrets {
		if !bytes.Contains(*config, []byte(value)) {
			t.Fatal("trusted projection lost profile credential")
		}
	}
	if !bytes.Contains(*config, []byte("edited-device")) || bytes.Contains(*config, []byte("sbox:v1:")) {
		t.Fatal("profile metadata or trusted projection changed")
	}
	if err := db.First(&stored, stored.Id).Error; err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), stored.Options...)
	nested["AUTH_TOKEN"] = map[string]string{"intent": "replace", "value": "private-alias-canary"}
	if err := save("edit", editable); err == nil {
		t.Fatal("credential alias bypassed the nested owner")
	}
	if err := db.First(&stored, stored.Id).Error; err != nil || !bytes.Equal(before, stored.Options) {
		t.Fatal("rejected alias changed canonical profile")
	}
}
