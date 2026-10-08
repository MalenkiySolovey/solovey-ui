package formats

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestJSONExtensionUpgradePreservesCustomPolicyAndResolver(t *testing.T) {
	source := []byte(`{"dns":{"servers":[{"type":"udp","tag":"dns","server":"127.0.0.1"}]},"default_domain_resolver":{"server":"dns","disable_cache":false,"rewrite_ttl":0},"rule_set":[{"tag":"geosite-private","type":"remote","url":"https://operator.example/custom.srs","download_detour":"direct","update_interval":"2h"},{"tag":"custom","type":"inline","rules":[{"domain":"custom.example"}]}],"rules":[{"rule_set":"custom","action":"reject"}],"custom":{"blank":"","false":false}}`)
	prepared, err := PrepareJSONExtensionUpgrade(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareJSONExtensionUpgrade(prepared.Candidate)
	if err != nil || !bytes.Equal(second.Candidate, prepared.Candidate) {
		t.Fatal("template migration is not idempotent")
	}
	if _, err := CanonicalJSONExtension(prepared.Candidate); err != nil {
		t.Fatal(err)
	}
	generated, err := RenderJSON([]map[string]interface{}{{"type": "direct", "tag": "direct"}, {"type": "selector", "tag": "proxy", "outbounds": []string{"direct"}}}, JSONOptions{Extension: string(prepared.Candidate), DirectRules: true})
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]interface{}
	_ = json.Unmarshal([]byte(generated), &root)
	route := root["route"].(map[string]interface{})
	resolver := route["default_domain_resolver"].(map[string]interface{})
	if resolver["disable_cache"] != false || resolver["rewrite_ttl"] != float64(0) || resolver["server"] != "dns" {
		t.Fatal("resolver shape or false/zero was lost")
	}
	if root["http_clients"] == nil || route["default_http_client"] == nil {
		t.Fatal("shared HTTP definitions or refs disappeared")
	}
	sets := route["rule_set"].([]interface{})
	var custom map[string]interface{}
	for _, item := range sets {
		set := item.(map[string]interface{})
		if set["tag"] == "geosite-private" {
			custom = set
		}
	}
	if custom == nil || custom["url"] != "https://operator.example/custom.srs" || custom["update_interval"] != "2h" || custom["http_client"] == nil {
		t.Fatal("edited preset was replaced")
	}
	if len(sets) != 3 {
		t.Fatal("custom set was deleted or preset duplicated")
	}
}

func TestJSONExtensionManualPreservesSourceAndRetry(t *testing.T) {
	source := []byte(`{"default_domain_resolver":{"server":"missing","disable_cache":false}}`)
	prepared, err := PrepareJSONExtensionUpgrade(source)
	if err == nil || !bytes.Equal(prepared.Candidate, source) || !strings.Contains(err.Error(), "subJsonExt.route.default_domain_resolver") {
		t.Fatal("missing resolver was invented or source lost")
	}
	corrected := []byte(`{"dns":{"servers":[{"type":"udp","tag":"dns","server":"127.0.0.1"}]},"default_domain_resolver":{"server":"dns","disable_cache":false}}`)
	if _, err := CanonicalJSONExtension(corrected); err != nil {
		t.Fatal("corrected retry failed")
	}
}

func TestJSONDirectTemplateMergePreservesMultiTagDefinitions(t *testing.T) {
	existing := []interface{}{map[string]interface{}{"tag": []string{"geosite-private", "custom"}, "type": "remote", "url": "https://operator.example/{tag}.srs", "http_client": map[string]interface{}{"engine": "go", "version": 2}, "update_interval": "2h"}}
	merged := mergeDirectRuleSets(existing)
	if len(merged) != 2 || len(ruleSetTags(merged[0])) != 2 {
		t.Fatal("list record was split, lost or duplicated by scalar catalogue identity")
	}
	if merged[0].(map[string]interface{})["url"] != "https://operator.example/{tag}.srs" {
		t.Fatal("operator URL was overwritten")
	}
}
