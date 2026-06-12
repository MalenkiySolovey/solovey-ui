package service

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestSingBoxConfigBuilderBuildsRuntimeSectionsFromDB(t *testing.T) {
	initSettingTestDB(t)
	db := database.GetDB()
	if err := db.Create(&model.Inbound{
		Type:    "direct",
		Tag:     "builder-in",
		Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":18080}`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Outbound{
		Type:    "direct",
		Tag:     "builder-out",
		Options: json.RawMessage(`{}`),
	}).Error; err != nil {
		t.Fatal(err)
	}

	input := `{
  "log": { "level": "info" },
  "dns": { "servers": [], "rules": [] },
  "route": { "rules": [] },
  "experimental": {},
  "inbounds": [{ "tag": "stale-in" }],
  "outbounds": [{ "tag": "stale-out" }],
  "services": [{ "tag": "stale-service" }],
  "endpoints": [{ "tag": "stale-endpoint" }],
  "future_top_level": { "enabled": true }
}`

	rawConfig, err := NewSingBoxConfigBuilder(nil).Build(input)
	if err != nil {
		t.Fatal(err)
	}

	var config map[string]json.RawMessage
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		t.Fatal(err)
	}
	if _, ok := config["future_top_level"]; !ok {
		t.Fatalf("unknown top-level field was not preserved: %s", string(rawConfig))
	}
	assertConfigSectionHasTag(t, config, "inbounds", "builder-in")
	assertConfigSectionMissingTag(t, config, "inbounds", "stale-in")
	assertConfigSectionHasTag(t, config, "outbounds", "builder-out")
	assertConfigSectionMissingTag(t, config, "outbounds", "stale-out")
	assertConfigSectionMissingTag(t, config, "services", "stale-service")
	assertConfigSectionMissingTag(t, config, "endpoints", "stale-endpoint")
}

func assertConfigSectionHasTag(t *testing.T, config map[string]json.RawMessage, section string, tag string) {
	t.Helper()
	if !configSectionHasTag(t, config, section, tag) {
		t.Fatalf("%s did not contain tag %q: %s", section, tag, string(config[section]))
	}
}

func assertConfigSectionMissingTag(t *testing.T, config map[string]json.RawMessage, section string, tag string) {
	t.Helper()
	if configSectionHasTag(t, config, section, tag) {
		t.Fatalf("%s unexpectedly contained tag %q: %s", section, tag, string(config[section]))
	}
}

func configSectionHasTag(t *testing.T, config map[string]json.RawMessage, section string, tag string) bool {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(config[section], &rows); err != nil {
		t.Fatalf("unmarshal %s: %v", section, err)
	}
	for _, row := range rows {
		if row["tag"] == tag {
			return true
		}
	}
	return false
}
