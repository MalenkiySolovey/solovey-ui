package formats

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestConvertToClashMetaEnsuresUniqueProxyNames(t *testing.T) {
	outbounds := []map[string]interface{}{
		{
			"type":        "vless",
			"tag":         "",
			"server":      "a.example.com",
			"server_port": 443,
			"uuid":        "11111111-1111-4111-8111-111111111111",
		},
		{
			"type":        "vmess",
			"tag":         "",
			"server":      "b.example.com",
			"server_port": 8443,
			"uuid":        "22222222-2222-4222-8222-222222222222",
		},
		{
			"type":        "trojan",
			"tag":         "node",
			"server":      "c.example.com",
			"server_port": 443,
			"password":    "secret-a",
		},
		{
			"type":        "trojan",
			"tag":         "node",
			"server":      "d.example.com",
			"server_port": 443,
			"password":    "secret-b",
		},
	}

	got, err := RenderClash(outbounds, DefaultClashConfig)
	if err != nil {
		t.Fatal(err)
	}

	var config map[string]interface{}
	if err := yaml.Unmarshal([]byte(got), &config); err != nil {
		t.Fatal(err)
	}

	proxies, ok := config["proxies"].([]interface{})
	if !ok || len(proxies) != 4 {
		t.Fatalf("expected four proxies, got %#v", config["proxies"])
	}

	seen := make(map[string]bool, len(proxies))
	names := make([]string, 0, len(proxies))
	for index, raw := range proxies {
		proxy, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("proxy %d is not a map: %#v", index, raw)
		}
		name, _ := proxy["name"].(string)
		if name == "" {
			t.Fatalf("proxy %d has an empty name", index)
		}
		if seen[name] {
			t.Fatalf("proxy %d has duplicate name %q", index, name)
		}
		seen[name] = true
		names = append(names, name)
	}

	if names[0] != "vless-a.example.com-443" {
		t.Fatalf("first fallback name = %q, want vless-a.example.com-443", names[0])
	}
	if names[1] != "vmess-b.example.com-8443" {
		t.Fatalf("second fallback name = %q, want vmess-b.example.com-8443", names[1])
	}
	if names[2] != "node" || names[3] != "node-2" {
		t.Fatalf("duplicate tags were not disambiguated: %#v", names)
	}

	groups, ok := config["proxy-groups"].([]interface{})
	if !ok || len(groups) < 2 {
		t.Fatalf("expected proxy groups, got %#v", config["proxy-groups"])
	}
	for _, rawGroup := range groups[len(groups)-2:] {
		group, ok := rawGroup.(map[string]interface{})
		if !ok {
			t.Fatalf("proxy group is not a map: %#v", rawGroup)
		}
		proxyNames, ok := group["proxies"].([]interface{})
		if !ok {
			t.Fatalf("proxy group has no proxies list: %#v", group)
		}
		for _, rawName := range proxyNames {
			name, _ := rawName.(string)
			if name == "Auto" {
				continue
			}
			if !seen[name] {
				t.Fatalf("proxy group references unknown proxy %q; known=%#v", name, names)
			}
		}
	}
}
