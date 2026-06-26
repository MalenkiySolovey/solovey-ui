package formats

import (
	"testing"

	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"gopkg.in/yaml.v3"
)

func TestRenderClashPreservesAdaptedGroups(t *testing.T) {
	outbounds := []map[string]interface{}{
		{
			"type":        "vless",
			"tag":         "proxy-a",
			"server":      "edge.example.com",
			"server_port": 443,
			"uuid":        "11111111-1111-1111-1111-111111111111",
		},
		{
			"type":      "urltest",
			"tag":       "xray-auto",
			"outbounds": []string{"proxy-a"},
			"url":       "http://www.gstatic.com/generate_204",
			"interval":  "10m",
			"tolerance": 50,
		},
	}
	rendered, err := RenderClash(outbounds, DefaultClashConfig)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err := yaml.Unmarshal([]byte(rendered), &config); err != nil {
		t.Fatal(err)
	}
	groups, _ := config["proxy-groups"].([]interface{})
	for _, raw := range groups {
		group, _ := raw.(map[string]interface{})
		if group["name"] != "xray-auto" {
			continue
		}
		if group["type"] != "url-test" {
			t.Fatalf("group = %#v", group)
		}
		refs, _ := group["proxies"].([]interface{})
		if len(refs) != 1 || refs[0] != "proxy-a" {
			t.Fatalf("group refs = %#v", group["proxies"])
		}
		return
	}
	t.Fatalf("adapted group missing from clash output: %#v", groups)
}

func TestRenderClashUsesGroupSourceMetadataWhenAvailable(t *testing.T) {
	rendered, err := RenderClash([]map[string]interface{}{
		{
			"type":        "vless",
			"tag":         "proxy-a",
			"server":      "edge.example.com",
			"server_port": 443,
			"uuid":        "11111111-1111-1111-1111-111111111111",
		},
		{
			"type":      "urltest",
			"tag":       "Balance",
			"outbounds": []string{"proxy-a"},
			subcanonical.MetadataKey: map[string]interface{}{
				"source_format":  subcanonical.FormatClash,
				"source_feature": "proxy-groups",
				"source_type":    "load-balance",
				"target_type":    "urltest",
				"strategy":       "round-robin",
			},
		},
		{
			"type":      "selector",
			"tag":       "Relay",
			"outbounds": []string{"proxy-a"},
			subcanonical.MetadataKey: map[string]interface{}{
				"source_format":  subcanonical.FormatClash,
				"source_feature": "proxy-groups",
				"source_type":    "relay",
				"target_type":    "selector",
			},
		},
	}, DefaultClashConfig)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err := yaml.Unmarshal([]byte(rendered), &config); err != nil {
		t.Fatal(err)
	}
	groups, _ := config["proxy-groups"].([]interface{})
	balance := clashGroupByName(t, groups, "Balance")
	if balance["type"] != "load-balance" || balance["strategy"] != "round-robin" {
		t.Fatalf("load-balance group = %#v", balance)
	}
	relay := clashGroupByName(t, groups, "Relay")
	if relay["type"] != "relay" {
		t.Fatalf("relay group = %#v", relay)
	}
}

func TestRenderClashUsesTargetGroupMetadataForConvertedClients(t *testing.T) {
	rendered, err := RenderClash([]map[string]interface{}{
		{
			"type":        "vless",
			"tag":         "proxy-a",
			"server":      "edge.example.com",
			"server_port": 443,
			"uuid":        "11111111-1111-1111-1111-111111111111",
		},
		{
			"type":      "urltest",
			"tag":       "Balance",
			"outbounds": []string{"proxy-a"},
			subcanonical.MetadataKey: []subcanonical.Adaptation{
				{
					SourceFormat:  subcanonical.FormatXray,
					SourceFeature: "routing.balancer",
					SourceType:    "balancer",
					TargetType:    "urltest",
					Strategy:      "random",
				},
				{
					SourceFeature: "client.conversion",
					SourceType:    "mihomo",
					TargetType:    "load-balance",
				},
			},
		},
	}, DefaultClashConfig)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err := yaml.Unmarshal([]byte(rendered), &config); err != nil {
		t.Fatal(err)
	}
	groups, _ := config["proxy-groups"].([]interface{})
	balance := clashGroupByName(t, groups, "Balance")
	if balance["type"] != "load-balance" || balance["strategy"] != "round-robin" {
		t.Fatalf("converted load-balance group = %#v", balance)
	}
}

func TestRenderClashPreservesSafeMihomoGroupMetadata(t *testing.T) {
	basicConfig := DefaultClashConfig + `
proxy-providers:
  provider-a:
    type: http
    url: https://example.com/sub.yaml
    path: ./providers/provider-a.yaml
    interval: 3600
`
	rendered, err := RenderClash([]map[string]interface{}{
		{
			"type":        "vless",
			"tag":         "proxy-a",
			"server":      "edge.example.com",
			"server_port": 443,
			"uuid":        "11111111-1111-1111-1111-111111111111",
		},
		{
			"type":      "urltest",
			"tag":       "Native Auto",
			"outbounds": []string{"proxy-a"},
			"url":       "http://www.gstatic.com/generate_204",
			"interval":  "10m",
			"tolerance": 50,
			"mihomo_group": map[string]interface{}{
				"type":                  "url-test",
				"name":                  "ignored-source-name",
				"proxies":               []interface{}{"missing-proxy"},
				"use":                   []interface{}{"provider-a", "missing-provider"},
				"url":                   "https://cp.cloudflare.com/generate_204",
				"interval":              120,
				"lazy":                  true,
				"timeout":               3000,
				"expected-status":       "204",
				"hidden":                true,
				"include-all-providers": true,
			},
		},
	}, basicConfig)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err := yaml.Unmarshal([]byte(rendered), &config); err != nil {
		t.Fatal(err)
	}
	groups, _ := config["proxy-groups"].([]interface{})
	group := clashGroupByName(t, groups, "Native Auto")
	if group["url"] != "https://cp.cloudflare.com/generate_204" || group["interval"] != 120 || group["lazy"] != true || group["timeout"] != 3000 {
		t.Fatalf("native group metadata was not preserved: %#v", group)
	}
	if group["expected-status"] != "204" || group["hidden"] != true {
		t.Fatalf("native group status/display metadata was not preserved: %#v", group)
	}
	refs, _ := group["proxies"].([]interface{})
	if len(refs) != 1 || refs[0] != "proxy-a" {
		t.Fatalf("native metadata should not override resolved proxy refs: %#v", group["proxies"])
	}
	providers, _ := group["use"].([]interface{})
	if len(providers) != 1 || providers[0] != "provider-a" {
		t.Fatalf("provider refs should be limited to declared providers: %#v", group["use"])
	}
	if _, exists := group["include-all-providers"]; exists {
		t.Fatalf("membership-expansion metadata should not be emitted blindly: %#v", group)
	}
}

func clashGroupByName(t *testing.T, groups []interface{}, name string) map[string]interface{} {
	t.Helper()
	for _, raw := range groups {
		group, _ := raw.(map[string]interface{})
		if group["name"] == name {
			return group
		}
	}
	t.Fatalf("group %q not found in %#v", name, groups)
	return nil
}
