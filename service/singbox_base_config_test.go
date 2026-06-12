package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/singboxconfig"
)

func TestDefaultSingBoxBaseConfigIsValidJSON(t *testing.T) {
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(defaultSingBoxBaseConfig), &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"log", "dns", "route", "experimental"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("default sing-box config is missing %q section", key)
		}
	}
}

func TestNormalizeSingBoxBaseConfigPreservesDNSAndRouteSections(t *testing.T) {
	config := json.RawMessage(`{"dns":{"servers":[{"tag":"dns-umbrella"}]},"route":{"rules":[{"action":"sniff"}]}}`)
	normalized, err := normalizeSingBoxBaseConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(normalized, "\n  \"dns\"") || !strings.Contains(normalized, "\n  \"route\"") {
		t.Fatalf("normalized config is not indented as expected: %s", normalized)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(normalized), &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"dns", "route"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("normalized config is missing %q section", key)
		}
	}
}

func TestParseSingBoxBaseConfigExposesDNSAndRouteSections(t *testing.T) {
	config := json.RawMessage(`{"log":{"level":"info"},"dns":{"servers":[]},"route":{"rules":[]}}`)
	doc, err := singboxconfig.ParseBaseConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if dns, ok := doc.DNS(); !ok || !strings.Contains(string(dns), `"servers"`) {
		t.Fatalf("DNS section was not exposed: ok=%v dns=%s", ok, string(dns))
	}
	if route, ok := doc.Route(); !ok || !strings.Contains(string(route), `"rules"`) {
		t.Fatalf("route section was not exposed: ok=%v route=%s", ok, string(route))
	}
}

func TestNormalizeSingBoxBaseConfigRejectsInvalidJSON(t *testing.T) {
	if _, err := normalizeSingBoxBaseConfig(json.RawMessage(`{"dns":`)); err == nil {
		t.Fatal("expected invalid sing-box config JSON to be rejected")
	}
}

func TestNormalizeSingBoxBaseConfigRejectsInvalidTopLevelShape(t *testing.T) {
	for _, config := range []json.RawMessage{
		nil,
		json.RawMessage(`null`),
		json.RawMessage(`[]`),
		json.RawMessage(`"text"`),
	} {
		if _, err := normalizeSingBoxBaseConfig(config); err == nil {
			t.Fatalf("expected non-object config to be rejected: %s", string(config))
		}
	}
}

func TestNormalizeSingBoxBaseConfigRejectsInvalidDNSAndRouteShape(t *testing.T) {
	tests := []struct {
		name    string
		config  json.RawMessage
		wantErr string
	}{
		{
			name:    "dns is not object",
			config:  json.RawMessage(`{"dns":[]}`),
			wantErr: "config.dns must be a JSON object",
		},
		{
			name:    "dns servers is not array",
			config:  json.RawMessage(`{"dns":{"servers":{}}}`),
			wantErr: "config.dns.servers must be a JSON array",
		},
		{
			name:    "dns rules is not array",
			config:  json.RawMessage(`{"dns":{"rules":{}}}`),
			wantErr: "config.dns.rules must be a JSON array",
		},
		{
			name:    "route is not object",
			config:  json.RawMessage(`{"route":[]}`),
			wantErr: "config.route must be a JSON object",
		},
		{
			name:    "route rules is not array",
			config:  json.RawMessage(`{"route":{"rules":{}}}`),
			wantErr: "config.route.rules must be a JSON array",
		},
		{
			name:    "route rule set is not array",
			config:  json.RawMessage(`{"route":{"rule_set":{}}}`),
			wantErr: "config.route.rule_set must be a JSON array",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeSingBoxBaseConfig(tt.config)
			if err == nil {
				t.Fatal("expected config shape to be rejected")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestNormalizeSingBoxBaseConfigRejectsDuplicateTaggedSections(t *testing.T) {
	tests := []struct {
		name    string
		config  json.RawMessage
		wantErr string
	}{
		{
			name:    "dns servers duplicate tag",
			config:  json.RawMessage(`{"dns":{"servers":[{"tag":"main"},{"tag":"main"}]}}`),
			wantErr: `config.dns.servers has duplicate tag "main"`,
		},
		{
			name:    "route rule set duplicate tag",
			config:  json.RawMessage(`{"route":{"rule_set":[{"tag":"geo"},{"tag":"geo"}]}}`),
			wantErr: `config.route.rule_set has duplicate tag "geo"`,
		},
		{
			name:    "dns server tag is not string",
			config:  json.RawMessage(`{"dns":{"servers":[{"tag":42}]}}`),
			wantErr: "config.dns.servers[0].tag must be a string",
		},
		{
			name:    "route rule set entry is not object",
			config:  json.RawMessage(`{"route":{"rule_set":["geo"]}}`),
			wantErr: "config.route.rule_set must contain JSON objects",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeSingBoxBaseConfig(tt.config)
			if err == nil {
				t.Fatal("expected tagged section to be rejected")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestSingBoxBaseConfigStoreSetValidatesAndNormalizesConfig(t *testing.T) {
	settingService := initSettingTestDB(t)
	store := NewSingBoxBaseConfigStore(settingService)

	if err := store.Set(`{"dns":{"servers":[]},"route":{"rules":[]}}`); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Get()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved, "\n  \"dns\"") || !strings.Contains(saved, "\n  \"route\"") {
		t.Fatalf("set config was not normalized: %s", saved)
	}

	if err := store.Set(`{"dns":{"servers":{}}}`); err == nil {
		t.Fatal("expected invalid config to be rejected")
	}
}

func TestSingBoxBaseConfigStoreSaveCreatesMissingConfigSetting(t *testing.T) {
	settingService := initSettingTestDB(t)
	tx := database.GetDB().Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}

	config := json.RawMessage(`{"dns":{"servers":[{"tag":"dns-umbrella"}]},"route":{"rules":[{"action":"sniff"}]}}`)
	if err := NewSingBoxBaseConfigStore(settingService).Save(tx, config); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}

	var saved string
	if err := database.GetDB().Model(&model.Setting{}).Select("value").Where("key = ?", "config").Scan(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved, `"dns"`) || !strings.Contains(saved, `"route"`) {
		t.Fatalf("saved config does not contain DNS and route data: %s", saved)
	}
}
