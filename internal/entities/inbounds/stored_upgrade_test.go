package entityinbounds

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestStoredTLSClientProjectionCleanupIsProtocolIndependent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := db.AutoMigrate(&model.Inbound{}, &model.Tls{}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"vless", "trojan", "tuic"} {
		row := model.Inbound{Type: kind, Tag: kind, Options: json.RawMessage(`{}`), OutJson: json.RawMessage(`{"server":"fixture.invalid","custom":false,"tls":{"insecure":false,"ech":{"enabled":true,"config":["fixture-config"],"pq_signature_schemes_enabled":true,"dynamic_record_sizing_disabled":null}}}`)}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error { _, err := StageStoredUpgrade(tx); return err }); err != nil {
		t.Fatal(err)
	}
	var rows []model.Inbound
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		var got struct {
			Server string
			Custom bool
			TLS    struct {
				Insecure bool
				ECH      map[string]json.RawMessage
			}
		}
		if err := json.Unmarshal(row.OutJson, &got); err != nil {
			t.Fatal(err)
		}
		if got.Server != "fixture.invalid" || got.Custom || got.TLS.Insecure || string(got.TLS.ECH["config"]) != `["fixture-config"]` {
			t.Fatal("unrelated projection intent changed")
		}
		for _, key := range []string{"pq_signature_schemes_enabled", "dynamic_record_sizing_disabled"} {
			if _, exists := got.TLS.ECH[key]; exists {
				t.Fatal("removed ECH option retained for " + row.Type)
			}
		}
		candidate, _, err := prepareOutboundTLSUpgrade("fixture", row.OutJson)
		if err != nil || !bytes.Equal(candidate, row.OutJson) {
			t.Fatal("cleanup is not idempotent", err)
		}
	}
}

func TestStoredTLSProjectionRejectsAmbiguousLegacyFlags(t *testing.T) {
	source := json.RawMessage(`{"tls":{"ech":{"pq_signature_schemes_enabled":"operator-choice"}}}`)
	got, _, err := prepareOutboundTLSUpgrade("fixture", source)
	if err == nil || !bytes.Equal(got, source) {
		t.Fatal("ambiguous operator input was silently rewritten")
	}
}

func TestGeneratedTLSClientDoesNotReintroduceLegacyECH(t *testing.T) {
	row := model.Inbound{Type: "vless", TlsId: 1, Tls: &model.Tls{Server: json.RawMessage(`{"enabled":true,"ech":{"enabled":true,"key_path":"fixture-key","pq_signature_schemes_enabled":true}}`), Client: json.RawMessage(`{"ech":{"enabled":true,"config":["fixture-config"],"dynamic_record_sizing_disabled":false}}`)}, Options: json.RawMessage(`{}`)}
	if err := FillOutboundJSON(&row, "fixture.invalid"); err != nil {
		t.Fatal(err)
	}
	var got struct {
		TLS struct{ ECH map[string]json.RawMessage }
	}
	if err := json.Unmarshal(row.OutJson, &got); err != nil {
		t.Fatal(err)
	}
	var configs []string
	if err := json.Unmarshal(got.TLS.ECH["config"], &configs); err != nil {
		t.Fatal(err)
	}
	if len(configs) != 1 || configs[0] != "fixture-config" || string(got.TLS.ECH["enabled"]) != "true" {
		t.Fatal("active ECH client options lost")
	}
	for _, key := range []string{"pq_signature_schemes_enabled", "dynamic_record_sizing_disabled", "key_path"} {
		if _, present := got.TLS.ECH[key]; present {
			t.Fatal("server-only or removed option was generated")
		}
	}
}
