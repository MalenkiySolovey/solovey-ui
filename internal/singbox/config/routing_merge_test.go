package singboxconfig

import (
	"encoding/json"
	"testing"
)

func TestMergeMappedRoutingSeedsDefaultBaseConfig(t *testing.T) {
	mapped := map[string]any{
		"route": map[string]any{
			"rules": []any{map[string]any{"action": "route", "outbound": "direct"}},
		},
	}
	merged, changed, err := MergeMappedRouting("", mapped)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected routing merge to report a change")
	}

	var cfg map[string]json.RawMessage
	if err := json.Unmarshal([]byte(merged), &cfg); err != nil {
		t.Fatal(err)
	}
	var route map[string]any
	if err := json.Unmarshal(cfg["route"], &route); err != nil {
		t.Fatal(err)
	}
	rules, ok := route["rules"].([]any)
	if !ok {
		t.Fatalf("route rules missing after merge: %v", route)
	}
	if len(rules) != 3 {
		t.Fatalf("route rules count = %d, want default 2 plus migrated 1: %v", len(rules), rules)
	}
}

func TestMergeMappedRoutingDeduplicatesRulesAndTaggedCollections(t *testing.T) {
	current := `{
  "dns": {
    "servers": [{"tag": "main"}],
    "rules": [{"server": "main", "domain": ["example.com"]}]
  },
  "route": {
    "rules": [{"action": "sniff"}],
    "rule_set": [{"tag": "geo"}]
  }
}`
	mapped := map[string]any{
		"dns": map[string]any{
			"servers": []any{
				map[string]any{"tag": "main"},
				map[string]any{"tag": "backup"},
			},
			"rules": []any{
				map[string]any{"server": "main", "domain": []any{"example.com"}},
				map[string]any{"server": "backup", "domain": []any{"example.org"}},
			},
			"final": "backup",
		},
		"route": map[string]any{
			"rules": []any{
				map[string]any{"action": "sniff"},
				map[string]any{"action": "route", "outbound": "direct"},
			},
			"rule_set": []any{
				map[string]any{"tag": "geo"},
				map[string]any{"tag": "geo-extra"},
			},
		},
	}

	merged, changed, err := MergeMappedRouting(current, mapped)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected routing merge to report a change")
	}

	var cfg map[string]json.RawMessage
	if err := json.Unmarshal([]byte(merged), &cfg); err != nil {
		t.Fatal(err)
	}
	var dns map[string]any
	if err := json.Unmarshal(cfg["dns"], &dns); err != nil {
		t.Fatal(err)
	}
	if got := len(dns["servers"].([]any)); got != 2 {
		t.Fatalf("DNS server count = %d, want 2: %v", got, dns["servers"])
	}
	if got := len(dns["rules"].([]any)); got != 2 {
		t.Fatalf("DNS rule count = %d, want 2: %v", got, dns["rules"])
	}
	if dns["final"] != "backup" {
		t.Fatalf("DNS final = %v, want backup", dns["final"])
	}

	var route map[string]any
	if err := json.Unmarshal(cfg["route"], &route); err != nil {
		t.Fatal(err)
	}
	if got := len(route["rules"].([]any)); got != 2 {
		t.Fatalf("route rule count = %d, want 2: %v", got, route["rules"])
	}
	if got := len(route["rule_set"].([]any)); got != 2 {
		t.Fatalf("route rule_set count = %d, want 2: %v", got, route["rule_set"])
	}
}

func TestMergeMappedRoutingKeepsExistingDNSKnobs(t *testing.T) {
	current := `{"dns":{"servers":[],"final":"current"}}`
	mapped := map[string]any{
		"dns": map[string]any{
			"servers": []any{map[string]any{"tag": "new"}},
			"final":   "new",
		},
	}
	merged, changed, err := MergeMappedRouting(current, mapped)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected routing merge to report a change")
	}

	var cfg map[string]json.RawMessage
	if err := json.Unmarshal([]byte(merged), &cfg); err != nil {
		t.Fatal(err)
	}
	var dns map[string]any
	if err := json.Unmarshal(cfg["dns"], &dns); err != nil {
		t.Fatal(err)
	}
	if dns["final"] != "current" {
		t.Fatalf("DNS final = %v, want current", dns["final"])
	}
}

func TestMergeMappedRoutingReportsNoChangeForEmptyMappedConfig(t *testing.T) {
	merged, changed, err := MergeMappedRouting(`{"dns":{},"route":{}}`, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatalf("expected no change, merged config: %s", merged)
	}
	if merged != "" {
		t.Fatalf("merged config = %q, want empty when unchanged", merged)
	}
}
