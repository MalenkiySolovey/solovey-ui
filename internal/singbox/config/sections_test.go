package singboxconfig

import (
	"encoding/json"
	"testing"
)

func TestBuildRuntimeConfigSetsRuntimeSections(t *testing.T) {
	base := json.RawMessage(`{
  "log": { "level": "info" },
  "dns": { "servers": [], "rules": [] },
  "route": { "rules": [] },
  "experimental": {},
  "certificate": { "store": "mozilla" },
  "inbounds": [{ "tag": "stale-in" }],
  "outbounds": [{ "tag": "stale-out" }],
  "services": [{ "tag": "stale-service" }],
  "endpoints": [{ "tag": "stale-endpoint" }],
  "future_top_level": { "enabled": true }
}`)

	built, err := BuildRuntimeConfig(base, RuntimeSections{
		Inbounds:  []map[string]any{{"tag": "runtime-in"}},
		Outbounds: []map[string]any{{"tag": "runtime-out"}},
		Services:  []map[string]any{{"tag": "runtime-service"}},
		Endpoints: []map[string]any{{"tag": "runtime-endpoint"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var config map[string]json.RawMessage
	if err := json.Unmarshal(built, &config); err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"certificate", "future_top_level", "dns", "route"} {
		if _, ok := config[section]; !ok {
			t.Fatalf("%s section was not preserved: %s", section, string(built))
		}
	}
	assertSectionTag(t, config, SectionInbounds, "runtime-in", true)
	assertSectionTag(t, config, SectionInbounds, "stale-in", false)
	assertSectionTag(t, config, SectionOutbounds, "runtime-out", true)
	assertSectionTag(t, config, SectionOutbounds, "stale-out", false)
	assertSectionTag(t, config, SectionServices, "runtime-service", true)
	assertSectionTag(t, config, SectionServices, "stale-service", false)
	assertSectionTag(t, config, SectionEndpoints, "runtime-endpoint", true)
	assertSectionTag(t, config, SectionEndpoints, "stale-endpoint", false)
}

func TestBuildRuntimeConfigRejectsInvalidBaseJSON(t *testing.T) {
	if _, err := BuildRuntimeConfig(json.RawMessage(`not json`), RuntimeSections{}); err == nil {
		t.Fatal("BuildRuntimeConfig must reject malformed base JSON")
	}
}

func assertSectionTag(t *testing.T, config map[string]json.RawMessage, section string, tag string, want bool) {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(config[section], &rows); err != nil {
		t.Fatalf("unmarshal %s: %v", section, err)
	}
	for _, row := range rows {
		if row["tag"] == tag {
			if !want {
				t.Fatalf("%s unexpectedly contained %q: %s", section, tag, string(config[section]))
			}
			return
		}
	}
	if want {
		t.Fatalf("%s did not contain %q: %s", section, tag, string(config[section]))
	}
}
