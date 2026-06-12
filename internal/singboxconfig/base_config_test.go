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
		{name: "top level is array", config: json.RawMessage(`[]`), wantErr: "config must be a JSON object"},
		{name: "dns is array", config: json.RawMessage(`{"dns":[]}`), wantErr: "config.dns must be a JSON object"},
		{name: "dns servers is object", config: json.RawMessage(`{"dns":{"servers":{}}}`), wantErr: "config.dns.servers must be a JSON array"},
		{name: "route rules is object", config: json.RawMessage(`{"route":{"rules":{}}}`), wantErr: "config.route.rules must be a JSON array"},
		{name: "duplicate DNS server tag", config: json.RawMessage(`{"dns":{"servers":[{"tag":"main"},{"tag":"main"}]}}`), wantErr: `config.dns.servers has duplicate tag "main"`},
		{name: "duplicate route rule set tag", config: json.RawMessage(`{"route":{"rule_set":[{"tag":"geo"},{"tag":"geo"}]}}`), wantErr: `config.route.rule_set has duplicate tag "geo"`},
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
