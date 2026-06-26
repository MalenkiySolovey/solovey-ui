package formats

import (
	"encoding/json"
	"testing"
)

func TestJsonServiceAddFragmentToSupportedOutbounds(t *testing.T) {
	outbounds := []map[string]interface{}{
		{"type": "selector", "tag": "proxy"},
		{"type": "vless", "tag": "vless-out"},
		{"type": "vmess", "tag": "vmess-out"},
		{"type": "trojan", "tag": "trojan-out"},
		{"type": "shadowsocks", "tag": "ss-out"},
	}
	config := map[string]interface{}{
		"outbounds": &outbounds,
	}
	if err := ApplyJSONOptions(&config, JSONOptions{Fragment: `{"enabled":true,"packets":"tlshello"}`}); err != nil {
		t.Fatal(err)
	}

	for _, outbound := range outbounds {
		_, hasFragment := outbound["fragment"]
		switch outbound["type"] {
		case "vless", "vmess", "trojan":
			if !hasFragment {
				t.Fatalf("%s outbound is missing fragment: %#v", outbound["type"], outbound)
			}
		default:
			if hasFragment {
				t.Fatalf("%s outbound should not receive fragment: %#v", outbound["type"], outbound)
			}
		}
	}
}

func TestJsonServiceAddNoisesToSupportedOutbounds(t *testing.T) {
	outbounds := []map[string]interface{}{
		{"type": "selector", "tag": "proxy"},
		{"type": "vless", "tag": "vless-out"},
		{"type": "vmess", "tag": "vmess-out"},
		{"type": "trojan", "tag": "trojan-out"},
		{"type": "shadowsocks", "tag": "ss-out"},
	}
	config := map[string]interface{}{
		"outbounds": &outbounds,
	}
	if err := ApplyJSONOptions(&config, JSONOptions{Noises: `[{"type":"rand","packet":"tlshello"}]`}); err != nil {
		t.Fatal(err)
	}

	for _, outbound := range outbounds {
		_, hasNoises := outbound["noises"]
		switch outbound["type"] {
		case "vless", "vmess", "trojan":
			if !hasNoises {
				t.Fatalf("%s outbound is missing noises: %#v", outbound["type"], outbound)
			}
		default:
			if hasNoises {
				t.Fatalf("%s outbound should not receive noises: %#v", outbound["type"], outbound)
			}
		}
	}
}

func TestJsonServiceMuxToggle(t *testing.T) {
	outbounds := []map[string]interface{}{
		{"type": "vless", "tag": "vless-out"},
		{"type": "shadowsocks", "tag": "ss-out"},
	}
	config := map[string]interface{}{
		"outbounds": &outbounds,
	}
	if err := ApplyJSONOptions(&config, JSONOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, outbound := range outbounds {
		if _, ok := outbound["multiplex"]; ok {
			t.Fatalf("mux should be absent when subJsonMux=false: %#v", outbound)
		}
	}

	outbounds = []map[string]interface{}{
		{"type": "selector", "tag": "proxy"},
		{"type": "vless", "tag": "vless-out"},
		{"type": "vmess", "tag": "vmess-out"},
		{"type": "trojan", "tag": "trojan-out"},
		{"type": "shadowsocks", "tag": "ss-out"},
		{"type": "hysteria2", "tag": "hy2-out"},
	}
	config = map[string]interface{}{
		"outbounds": &outbounds,
	}
	if err := ApplyJSONOptions(&config, JSONOptions{Mux: true}); err != nil {
		t.Fatal(err)
	}

	for _, outbound := range outbounds {
		mux, hasMux := outbound["multiplex"]
		switch outbound["type"] {
		case "vless", "vmess", "trojan", "shadowsocks":
			if !hasMux {
				t.Fatalf("%s outbound is missing mux: %#v", outbound["type"], outbound)
			}
			muxMap, ok := mux.(map[string]interface{})
			if !ok || muxMap["enabled"] != true || muxMap["protocol"] != "smux" {
				t.Fatalf("unexpected mux settings for %s: %#v", outbound["type"], mux)
			}
		default:
			if hasMux {
				t.Fatalf("%s outbound should not receive mux: %#v", outbound["type"], outbound)
			}
		}
	}
}

func TestJsonServiceDirectRulesToggle(t *testing.T) {
	config := map[string]interface{}{}
	if err := ApplyJSONOptions(&config, JSONOptions{}); err != nil {
		t.Fatal(err)
	}
	route := config["route"].(map[string]interface{})
	if _, ok := route["rule_set"]; ok {
		t.Fatalf("direct rule_sets should be absent when subJsonDirectRules=false: %#v", route)
	}

	config = map[string]interface{}{}
	if err := ApplyJSONOptions(&config, JSONOptions{DirectRules: true}); err != nil {
		t.Fatal(err)
	}
	route = config["route"].(map[string]interface{})
	rules := route["rules"].([]interface{})
	if len(rules) < 2 {
		t.Fatalf("expected direct rule after sniff rule: %#v", rules)
	}
	directRule := rules[1].(map[string]interface{})
	if directRule["outbound"] != "direct" || directRule["action"] != "route" {
		t.Fatalf("unexpected direct rule: %#v", directRule)
	}
	ruleSetTags, ok := directRule["rule_set"].([]string)
	if !ok || len(ruleSetTags) != 2 || ruleSetTags[0] != "geosite-private" || ruleSetTags[1] != "geoip-private" {
		t.Fatalf("unexpected direct rule sets: %#v", directRule["rule_set"])
	}
	ruleSets := route["rule_set"].([]interface{})
	if !hasRuleSetTag(ruleSets, "geosite-private") || !hasRuleSetTag(ruleSets, "geoip-private") {
		t.Fatalf("private rule_sets missing: %#v", ruleSets)
	}
}

func TestRenderJSONAdaptsPanelFailoverToSelector(t *testing.T) {
	rendered, err := RenderJSON([]map[string]interface{}{
		{"type": "direct", "tag": "direct"},
		{
			"type":      "failover",
			"tag":       "remote-failover",
			"outbounds": []string{"node-a"},
			"failover": map[string]interface{}{
				"enabled":      true,
				"probe_target": "https://example.com/",
			},
		},
	}, JSONOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(rendered), &config); err != nil {
		t.Fatal(err)
	}
	outbounds, _ := config["outbounds"].([]interface{})
	group, _ := outbounds[1].(map[string]interface{})
	if group["type"] != "selector" || group["default"] != "node-a" {
		t.Fatalf("failover was not rendered as selector: %#v", group)
	}
	if _, leaked := group["failover"]; leaked {
		t.Fatalf("panel failover metadata leaked into client JSON: %#v", group)
	}
	members, _ := group["outbounds"].([]interface{})
	if len(members) != 2 || members[0] != "node-a" || members[1] != "direct" {
		t.Fatalf("selector members = %#v", group["outbounds"])
	}
}

func hasRuleSetTag(ruleSets []interface{}, tag string) bool {
	for _, ruleSet := range ruleSets {
		if got, ok := ruleSetTag(ruleSet); ok && got == tag {
			return true
		}
	}
	return false
}
