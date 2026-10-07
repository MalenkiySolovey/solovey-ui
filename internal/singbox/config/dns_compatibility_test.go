package singboxconfig

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
)

func TestDNSUpgradePreservesPathsAndPreimages(t *testing.T) {
	tests := []struct {
		name, input, code, contains string
		blocked                     bool
	}{
		{"literal direct", `{"dns":{"servers":[{"tag":"a","address":"udp://127.0.0.1:15353","detour":"direct"}]},"outbounds":[{"type":"direct","tag":"direct"}]}`, "dns_server_migrated", `"server_port":15353`, false},
		{"frozen proxy", `{"dns":{"servers":[{"tag":"a","address":"127.0.0.1"}]},"route":{"final":"proxy"},"outbounds":[{"type":"direct","tag":"direct"},{"type":"socks","tag":"proxy","server":"127.0.0.1","server_port":1080}]}`, "dns_server_migrated", `"detour":"proxy"`, false},
		{"custom direct", `{"dns":{"servers":[{"tag":"a","address":"127.0.0.1","detour":"bound"}]},"outbounds":[{"type":"direct","tag":"bound","bind_interface":"example0"}]}`, "dns_server_migrated", `"detour":"bound"`, false},
		{"IPv6 TLS", `{"dns":{"servers":[{"tag":"a","address":"tls://[::1]:1853","detour":"direct"}]},"outbounds":[{"type":"direct","tag":"direct"}]}`, "dns_server_migrated", `"server":"::1"`, false},
		{"HTTPS path", `{"dns":{"servers":[{"tag":"a","address":"https://127.0.0.1:1443/custom","detour":"direct"}]},"outbounds":[{"type":"direct","tag":"direct"}]}`, "dns_server_migrated", `"path":"/custom"`, false},
		{"bootstrap strategy", `{"dns":{"servers":[{"type":"udp","tag":"boot","server":"127.0.0.1"},{"tag":"a","address":"tls://dns.example","address_resolver":"boot","address_strategy":"ipv4_only","detour":"direct"}]},"outbounds":[{"type":"direct","tag":"direct"}]}`, "dns_server_migrated", `"domain_resolver":{"server":"boot","strategy":"ipv4_only"}`, false},
		{"strategy preserved", `{"dns":{"servers":[{"tag":"a","address":"127.0.0.1","strategy":"ipv4_only"}]}}`, "dns_legacy_strategy_manual", "", true},
		{"no selected outbound", `{"dns":{"servers":[{"tag":"a","address":"127.0.0.1"}]}}`, "dns_legacy_detour_manual", "", true},
		{"bootstrap self", `{"dns":{"servers":[{"tag":"a","address":"tls://dns.example","address_resolver":"a","detour":"direct"}]},"outbounds":[{"type":"direct","tag":"direct"}]}`, "dns_bootstrap_reference_manual", "", true},
		{"unknown", `{"dns":{"servers":[{"address":"unknown://dns.example"}]}}`, "dns_legacy_transport_unsupported", "", true},
		{"dual representation", `{"dns":{"servers":[{"type":"udp","address":"127.0.0.1","server":"127.0.0.2"}]}}`, "dns_representation_conflict", "", true},
		{"host port cannot recover", `{"dns":{"servers":[{"type":"udp","server":"[::1]:53"}]}}`, "dns_host_port_ambiguous", "", true},
		{"legacy local", `{"dns":{"servers":[{"address":"local"}]}}`, "dns_legacy_local_manual", "", true},
		{"local generic", `{"dns":{"servers":[{"type":"local","tag":"system"}]}}`, "dns_local_behavior_changed", `"type":"local"`, false},
		{"local unicast intent", `{"dns":{"servers":[{"type":"local","tag":"system"}],"rules":[{"domain_suffix":["local"],"server":"system"}]}}`, "dns_local_policy_changed", "", true},
		{"new internal reachability", `{"dns":{"servers":[{"type":"udp","tag":"a","server":"127.0.0.1"}],"rules":[{"query_type":["A"],"server":"a"}]},"outbounds":[{"type":"socks","server":"proxy.example","server_port":1080}]}`, "dns_internal_query_policy_manual", "", true},
		{"explicit bypass", `{"dns":{"servers":[{"type":"udp","tag":"a","server":"127.0.0.1"}],"rules":[{"query_type":["A"],"server":"a"}]},"outbounds":[{"type":"socks","server":"proxy.example","server_port":1080,"domain_resolver":"a"}]}`, "dns_internal_query_matching_changed", `"domain_resolver":"a"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []byte(tt.input)
			p, err := PrepareDNSUpgrade(input)
			if (err != nil) != tt.blocked {
				t.Fatalf("blocked=%v, findings=%+v", err != nil, p.Findings)
			}
			if !hasDNSCode(p.Findings, tt.code) {
				t.Fatalf("missing %s: %+v", tt.code, p.Findings)
			}
			if !bytes.Equal(p.Original, input) {
				t.Fatal("source changed")
			}
			if tt.blocked {
				if !bytes.Equal(p.Candidate, input) {
					t.Fatal("failed candidate changed source")
				}
				return
			}
			if !strings.Contains(string(p.Candidate), tt.contains) {
				t.Fatal("required semantics missing")
			}
			second, err := PrepareDNSUpgrade(p.Candidate)
			if err != nil || !bytes.Equal(p.Candidate, second.Candidate) {
				t.Fatalf("not idempotent: %v", err)
			}
		})
	}
}

func TestDNSFakeIPRcodeAndCachePresence(t *testing.T) {
	for _, raw := range []string{"false", "null", `{"enabled":false,"inet4_range":"198.18.0.0/15"}`} {
		p, err := PrepareDNSUpgrade([]byte(`{"dns":{"fakeip":` + raw + `,"servers":[]}}`))
		if err != nil || bytes.Contains(p.Candidate, []byte(`"fakeip"`)) {
			t.Fatalf("inactive presence not removed: %v", err)
		}
	}
	active := []byte(`{"dns":{"fakeip":{"enabled":true,"inet4_range":"198.18.0.0/15","inet6_range":"fc00::/18"},"servers":[{"type":"udp","tag":"normal","server":"127.0.0.1"},{"tag":"fake","address":"fakeip"}],"rules":[{"domain_suffix":["example"],"server":"fake"}]}}`)
	p, err := PrepareDNSUpgrade(active)
	if err != nil || !bytes.Contains(p.Candidate, []byte(`"inet6_range":"fc00::/18"`)) {
		t.Fatal("active ranges lost")
	}
	for _, code := range []string{"success", "name_error", "refused"} {
		p, err := PrepareDNSUpgrade([]byte(`{"dns":{"servers":[{"tag":"deny","address":"rcode://` + code + `"}],"rules":[{"domain_suffix":["example"],"server":"deny"}]}}`))
		if err != nil || !bytes.Contains(p.Candidate, []byte(`"action":"predefined"`)) {
			t.Fatalf("rcode: %v", err)
		}
	}
	for _, rules := range []string{`"final":"deny"`, `"rules":[{"type":"logical","mode":"and","rules":[{"domain_suffix":["example"],"server":"deny"}]}]`} {
		input := []byte(`{"dns":{"servers":[{"tag":"deny","address":"rcode://refused"}],` + rules + `}}`)
		p, err := PrepareDNSUpgrade(input)
		if err == nil || !bytes.Equal(input, p.Candidate) || !hasDNSCode(p.Findings, "dns_rcode_manual") {
			t.Fatal("unsafe rcode accepted")
		}
	}
	for _, value := range []string{"true", "false"} {
		p, err := PrepareDNSUpgrade([]byte(`{"dns":{"independent_cache":` + value + `}}`))
		if err != nil || bytes.Contains(p.Candidate, []byte("independent_cache")) || !hasDNSCode(p.Findings, "dns_cache_partition_changed") {
			t.Fatal("no-op not explained")
		}
	}
	rdrc := []byte(`{"experimental":{"cache_file":{"enabled":true,"store_rdrc":true,"rdrc_timeout":"1h"}}}`)
	p, err = PrepareDNSUpgrade(rdrc)
	if err != nil || !bytes.Equal(p.Candidate, rdrc) || !hasDNSCode(p.Findings, "dns_rejection_cache_retained") {
		t.Fatal("rejection persistence widened")
	}
	input := []byte(`{"experimental":{"cache_file":{"store_rdrc":true,"store_dns":true}}}`)
	p, err = PrepareDNSUpgrade(input)
	if err == nil || !bytes.Equal(p.Candidate, input) || !hasDNSCode(p.Findings, "dns_cache_privacy_choice") {
		t.Fatal("dual privacy intent accepted")
	}
}

func TestDNSRuleModeNamespaceAndBudget(t *testing.T) {
	tests := []struct {
		rules, sets, code string
		blocked           bool
	}{
		{`[{"ip_cidr":["192.0.2.0/24"],"invert":true,"server":"a","strategy":"ipv4_only"}]`, "", "dns_legacy_rule_mode_retained", false},
		{`[{"ip_cidr":["192.0.2.0/24"],"query_type":["A"],"server":"a"}]`, "", "dns_rule_mode_conflict", true},
		{`[{"action":"evaluate","tag":"response","server":"a","race":true},{"match_response":"response","ip_is_private":true,"action":"respond"}]`, "", "", false},
		{`[{"match_response":"later","action":"respond"},{"action":"evaluate","tag":"later","server":"a"}]`, "", "dns_evaluate_reference_missing", true},
		{`[{"action":"evaluate","tag":"r","server":"a"},{"action":"evaluate","tag":"r","server":"a"}]`, "", "dns_evaluate_tag_duplicate", true},
		{`[{"action":"evaluate","server":"fake"}]`, "", "dns_evaluate_fakeip", true},
		{`[{"action":"route","speculative":true,"server":"a"}]`, "", "dns_speculative_without_race", false},
		{`[{"response_rcode":0,"server":"a"}]`, "", "dns_response_context_required", true},
		{`[{"rule_set":["ip"],"server":"a"}]`, `[{"type":"inline","tag":"ip","rules":[{"ip_cidr":["192.0.2.0/24"]}]}]`, "dns_legacy_rule_mode_retained", false},
		{`[{"action":"evaluate","server":"a"},{"rule_set":["ip"],"match_response":true,"action":"respond"}]`, `[{"type":"inline","tag":"ip","rules":[{"ip_cidr":["192.0.2.0/24"]}]}]`, "", false},
	}
	for i, tt := range tests {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			input := `{"dns":{"servers":[{"type":"udp","tag":"a","server":"127.0.0.1"},{"type":"fakeip","tag":"fake","inet4_range":"198.18.0.0/15"}],"rules":` + tt.rules + `}`
			if tt.sets != "" {
				input += `,"route":{"rule_set":` + tt.sets + `}`
			}
			input += `}`
			p, err := PrepareDNSUpgrade([]byte(input))
			if (err != nil) != tt.blocked || (tt.code != "" && !hasDNSCode(p.Findings, tt.code)) {
				t.Fatalf("unexpected: %v %+v", err, p.Findings)
			}
		})
	}
	var node any = map[string]any{"domain": []string{"example"}}
	for i := 0; i < 70; i++ {
		node = map[string]any{"type": "logical", "mode": "and", "rules": []any{node}}
	}
	raw, _ := json.Marshal(map[string]any{"dns": map[string]any{"servers": []any{map[string]any{"type": "local", "tag": "local"}}, "rules": []any{node}}})
	p, err := PrepareDNSUpgrade(raw)
	if err == nil || !hasDNSCode(p.Findings, "dns_rule_budget") || !bytes.Equal(raw, p.Candidate) {
		t.Fatal("unbounded rule tree")
	}
}

func TestDNSNewInputRequiresExplicitUpgradeButAcceptsChosenLocalPolicy(t *testing.T) {
	for _, input := range []string{`{"dns":{"fakeip":null}}`, `{"dns":{"independent_cache":false}}`} {
		if _, err := ValidateDNSConfig([]byte(input)); err == nil {
			t.Fatal("silent upgrade on save")
		}
	}
	if _, err := ValidateDNSConfig([]byte(`{"dns":{"servers":[{"type":"local","tag":"local"}],"rules":[{"domain_suffix":["local"],"server":"local"}]}}`)); err != nil {
		t.Fatal("explicit new multicast choice rejected")
	}
	if len(EditorContract().DNSActions["evaluate"]) == 0 || len(EditorContract().TUNDNSModes) != 3 {
		t.Fatal("pinned editor contract missing")
	}
}

func hasDNSCode(findings []diagnostics.Finding, code string) bool {
	for _, f := range findings {
		if f.Code == code {
			return true
		}
	}
	return false
}
