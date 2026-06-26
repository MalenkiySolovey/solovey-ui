package singboxconfig

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultBaseConfigIsValidJSON(t *testing.T) {
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(DefaultBaseConfig), &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"log", "dns", "route", "experimental"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("default sing-box config is missing %q section", key)
		}
	}
}

func TestParseBaseConfigExposesDNSAndRouteSections(t *testing.T) {
	doc, err := ParseBaseConfig(json.RawMessage(`{"dns":{"servers":[]},"route":{"rules":[]}}`))
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

func TestNormalizeBaseConfigRejectsInvalidEditableSectionShapes(t *testing.T) {
	tests := []struct {
		name    string
		config  json.RawMessage
		wantErr string
	}{
		{name: "top level is empty", config: nil, wantErr: "config must be a JSON object"},
		{name: "top level is null", config: json.RawMessage(`null`), wantErr: "config must be a JSON object"},
		{name: "top level is array", config: json.RawMessage(`[]`), wantErr: "config must be a JSON object"},
		{name: "top level is string", config: json.RawMessage(`"text"`), wantErr: "config must be a JSON object"},
		{name: "dns is array", config: json.RawMessage(`{"dns":[]}`), wantErr: "config.dns must be a JSON object"},
		{name: "dns servers is object", config: json.RawMessage(`{"dns":{"servers":{}}}`), wantErr: "config.dns.servers must be a JSON array"},
		{name: "dns rules is object", config: json.RawMessage(`{"dns":{"rules":{}}}`), wantErr: "config.dns.rules must be a JSON array"},
		{name: "route is array", config: json.RawMessage(`{"route":[]}`), wantErr: "config.route must be a JSON object"},
		{name: "route rules is object", config: json.RawMessage(`{"route":{"rules":{}}}`), wantErr: "config.route.rules must be a JSON array"},
		{name: "route rule set is object", config: json.RawMessage(`{"route":{"rule_set":{}}}`), wantErr: "config.route.rule_set must be a JSON array"},
		{name: "duplicate DNS server tag", config: json.RawMessage(`{"dns":{"servers":[{"tag":"main"},{"tag":"main"}]}}`), wantErr: `config.dns.servers has duplicate tag "main"`},
		{name: "duplicate route rule set tag", config: json.RawMessage(`{"route":{"rule_set":[{"tag":"geo"},{"tag":"geo"}]}}`), wantErr: `config.route.rule_set has duplicate tag "geo"`},
		{name: "dns server tag is not string", config: json.RawMessage(`{"dns":{"servers":[{"tag":42}]}}`), wantErr: "config.dns.servers[0].tag must be a string"},
		{name: "route rule set entry is not object", config: json.RawMessage(`{"route":{"rule_set":["geo"]}}`), wantErr: "config.route.rule_set must contain JSON objects"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeBaseConfig(tt.config)
			if err == nil {
				t.Fatal("expected invalid config to be rejected")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestNormalizeBaseConfigPreservesUnknownTopLevelFields(t *testing.T) {
	normalized, err := NormalizeBaseConfig(json.RawMessage(`{"future_top_level":{"enabled":true},"dns":{"servers":[]},"route":{"rules":[]}}`))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(normalized), &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["future_top_level"]; !ok {
		t.Fatalf("unknown top-level field was dropped: %s", normalized)
	}
}
