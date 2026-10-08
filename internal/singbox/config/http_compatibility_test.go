package singboxconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
)

func TestHTTPMemoryBytesRejectsWrappingNegativeInput(t *testing.T) {
	for _, version := range []int{2, 3} {
		if _, unavailable := HTTPUnavailableVersions()[fmt.Sprint(version)]; unavailable {
			continue
		}
		for _, value := range []string{"-1", "0", `"1MB"`} {
			source := []byte(fmt.Sprintf(`{"http_clients":[{"tag":"sized","engine":"go","version":%d,"stream_receive_window":%s}]}`, version, value))
			prepared, err := PrepareHTTPUpgrade(source)
			if value == "-1" {
				if err == nil || !bytes.Equal(prepared.Candidate, source) {
					t.Fatal("negative size wrapped or lost its preimage")
				}
				found := false
				for _, finding := range prepared.Findings {
					found = found || finding.Code == "http_size_invalid" && finding.Path == "http_clients[0].stream_receive_window"
				}
				if !found {
					t.Fatal("actual HTTP size consumer has no fixed path/reason")
				}
			} else if err != nil {
				t.Fatal("zero or accepted unit was rejected", err)
			}
		}
	}
}

func TestHTTPReferencesUseCompleteCandidateCatalogue(t *testing.T) {
	stored := []byte(`{"http_clients":[{"tag":"shared","engine":"go","version":2,"detour":"proxy"}],"route":{"default_http_client":"shared"}}`)
	if _, err := ValidateHTTPConfig(stored); err != nil {
		t.Fatal("base-only validation invented an absent entity catalogue")
	}
	for _, raw := range []string{
		`{"http_clients":[{"tag":"shared","engine":"go","version":2,"detour":"missing"}],"outbounds":[]}`,
		`{"route":{"default_http_client":42}}`,
		`{"route":{"rule_set":[{"tag":"x","type":"remote","url":"file:///tmp/custom"}]}}`,
	} {
		prepared, err := PrepareHTTPUpgrade([]byte(raw))
		if err == nil || !bytes.Equal(prepared.Candidate, []byte(raw)) {
			t.Fatal("invalid reference lost preimage")
		}
	}
}

func TestHTTPUpgradeFreezesIndependentDownloadAndDefaultPolicies(t *testing.T) {
	raw := []byte(`{"route":{"final":"proxy","rule_set":[{"type":"remote","tag":"direct-set","url":"https://operator.example/custom.srs","download_detour":"direct","update_interval":"2h"},{"type":"remote","tag":"implicit-set","url":"https://operator.example/other.srs","http_client":{}}]},"outbounds":[{"type":"direct","tag":"direct"},{"type":"socks","tag":"proxy","server":"127.0.0.1","server_port":1080}]}`)
	result, err := PrepareHTTPUpgrade(raw)
	if err != nil {
		t.Fatal(err)
	}
	var root dnsObject
	_ = json.Unmarshal(result.Candidate, &root)
	var route dnsObject
	_ = json.Unmarshal(root["route"], &route)
	var sets, clients []dnsObject
	_ = json.Unmarshal(route["rule_set"], &sets)
	_ = json.Unmarshal(root["http_clients"], &clients)
	if len(clients) != 2 {
		t.Fatal("policies were duplicated or lost")
	}
	direct, proxy := dnsString(sets[0], "http_client"), dnsString(sets[1], "http_client")
	if direct == proxy || dnsString(route, "default_http_client") != proxy {
		t.Fatal("explicit direct changed the independent default")
	}
	if dnsString(sets[0], "url") != "https://operator.example/custom.srs" || dnsString(sets[0], "update_interval") != "2h" {
		t.Fatal("operator policy lost")
	}
	for _, client := range clients {
		if dnsString(client, "tag") == direct {
			if dnsString(client, "engine") != "go" || string(client["version"]) != "2" || activeDNSRaw(client["detour"]) {
				t.Fatal("direct must be nonempty explicit Go/version2")
			}
		} else if dnsString(client, "detour") != "proxy" {
			t.Fatal("proxy detour lost")
		}
	}
	second, err := PrepareHTTPUpgrade(result.Candidate)
	if err != nil || !bytes.Equal(second.Candidate, result.Candidate) {
		t.Fatal("HTTP migration is not byte idempotent")
	}
	if _, err := ValidateHTTPConfig(result.Candidate); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateHTTPConfig(raw); err == nil {
		t.Fatal("strict writes silently converted legacy download intent")
	}
}

func TestHTTPConflictsPreservePreimageAndSafeReasons(t *testing.T) {
	cases := []struct{ raw, code string }{
		{`{"route":{"rule_set":[{"tag":"test","type":"remote","url":"https://operator.example/custom.srs","download_detour":"proxy","http_client":{"engine":"go","version":2}}]}}`, "http_representation_conflict"},
		{`{"http_clients":[{"tag":"dup"},{"tag":"dup"}]}`, "http_client_tag_duplicate"},
		{`{"route":{"default_http_client":"missing"}}`, "http_client_reference_missing"},
		{`{"route":{"rule_set":[{"tag":"test","type":"remote","url":"https://operator.example/custom.srs","http_client":"missing"}]}}`, "http_client_reference_missing"},
		{`{"http_clients":[{"tag":"native","engine":"apple"}]}`, "http_engine_unavailable"},
		{`{"http_clients":[{"tag":"typed","engine":"go","version":2,"detour":"direct"}],"outbounds":[{"type":"direct","tag":"direct"}]}`, "http_empty_direct_detour"},
		{`{"route":{"rule_set":[{"tag":"test","type":"remote","url":"https://operator.example/custom.srs"}]}}`, "http_outbound_policy_manual"},
	}
	for _, test := range cases {
		t.Run(test.code, func(t *testing.T) {
			raw := []byte(test.raw)
			result, err := PrepareHTTPUpgrade(raw)
			if err == nil || !bytes.Equal(raw, result.Candidate) || result.Outcome != diagnostics.ManualRequired {
				t.Fatal("manual source was changed or activated")
			}
			found := false
			for _, finding := range result.Findings {
				found = found || finding.Code == test.code
			}
			if !found {
				t.Fatalf("missing structural reason %s", test.code)
			}
		})
	}
	_, err := ValidateHTTPConfig([]byte(`{"http_clients":[{"tag":"private-canary","version":9,"headers":{"X-Witness":["private-canary"]}}]}`))
	if err == nil || strings.Contains(err.Error(), "private-canary") {
		t.Fatal("submitted HTTP fields leaked through diagnostics")
	}
}

func TestHTTPUpgradeRetainsExplicitFalseAndZeroInDirectOptions(t *testing.T) {
	for _, fields := range []string{`"reuse_addr":false,"routing_mark":0`, `"udp_fragment":false`} {
		raw := []byte(`{"route":{"final":"direct","rule_set":[{"tag":"operator","type":"remote","url":"https://operator.example/custom.srs","download_detour":"direct"}]},"outbounds":[{"type":"direct","tag":"direct",` + fields + `}]}`)
		prepared, err := PrepareHTTPUpgrade(raw)
		if err != nil {
			t.Fatal(err)
		}
		var root dnsObject
		_ = json.Unmarshal(prepared.Candidate, &root)
		var clients []dnsObject
		_ = json.Unmarshal(root["http_clients"], &clients)
		if len(clients) != 1 {
			t.Fatal("direct policy duplicated")
		}
		wantDetour := fields == `"udp_fragment":false`
		if (dnsString(clients[0], "detour") == "direct") != wantDetour {
			t.Fatal("pinned nonempty direct distinction lost")
		}
		if !bytes.Contains(root["outbounds"], []byte(`false`)) {
			t.Fatal("operator false option erased")
		}
	}
}

func TestHTTPPolicyReuseAndCollision(t *testing.T) {
	policy := dnsObject{"engine": dnsJSON("go"), "version": dnsJSON(2)}
	tag, clients, err := ensureHTTPPolicy(nil, policy)
	if err != nil || len(clients) != 1 {
		t.Fatal("identity generation failed")
	}
	otherPolicy := dnsObject{"engine": dnsJSON("go"), "version": dnsJSON(2)}
	otherTag, again, err := ensureHTTPPolicy(clients, otherPolicy)
	if err != nil || tag != otherTag || len(again) != 1 {
		t.Fatal("compatible policy was not reused")
	}
	clients[0]["headers"] = dnsJSON(map[string][]string{"X-Witness": {"custom"}})
	if _, _, err := ensureHTTPPolicy(clients, otherPolicy); err == nil {
		t.Fatal("divergent identity selected a silent winner")
	}
}

func TestRuleSetListNamespaceAndHeadlessBudgets(t *testing.T) {
	valid := []byte(`{"route":{"rule_set":[{"type":"remote","tag":["a","b"],"url":"https://operator.example/{tag}.srs","http_client":{"engine":"go","version":2}}],"rules":[{"rule_set":"b","action":"reject"}]}}`)
	if _, err := ValidateHTTPConfig(valid); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ raw, code string }{
		{`{"route":{"rule_set":[{"type":"remote","tag":["a","b"],"url":"https://operator.example/{tag}.srs"},{"type":"inline","tag":"b","rules":[]}]}}`, "ruleset_tag_duplicate"},
		{`{"route":{"rule_set":[{"type":"remote","tag":["a","a"],"url":"https://operator.example/{tag}.srs"}]}}`, "ruleset_tag_duplicate"},
		{`{"route":{"rule_set":[{"type":"remote","tag":["a","b"],"url":"https://operator.example/shared.srs"}]}}`, "ruleset_schema_invalid"},
		{`{"route":{"rule_set":[{"type":"remote","tag":"a","url":"https://operator.example/a.srs","initial_path":"../../outside.srs"}]}}`, "ruleset_initial_path_unmanaged"},
		{`{"route":{"rules":[{"rule_set":"missing","action":"reject"}]}}`, "ruleset_reference_missing"},
	}
	for _, test := range cases {
		findings := RuleSetFindings([]byte(test.raw), false)
		found := false
		for _, finding := range findings {
			found = found || finding.Code == test.code
		}
		if !found {
			t.Fatalf("missing reason %s", test.code)
		}
	}
	branch := `{"domain":"example"}`
	for i := 0; i < 65; i++ {
		branch = `{"type":"logical","mode":"or","rules":[` + branch + `]}`
	}
	findings := RuleSetFindings([]byte(`{"route":{"rule_set":[{"type":"inline","tag":"deep","rules":[`+branch+`]}]}}`), false)
	if diagnostics.FirstError(findings) == nil || findings[0].Code != "rule_budget_exceeded" {
		t.Fatal("headless recursion reached official decoding before its owner budget")
	}
}
