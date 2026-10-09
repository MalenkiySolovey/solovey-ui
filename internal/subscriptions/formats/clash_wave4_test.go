package formats

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestClashWave4RejectsAbsentOutboundObject(t *testing.T) {
	if output, err := RenderClash([]map[string]any{nil}, DefaultClashConfig); err == nil || output != "" {
		t.Fatal("malformed outbound was exported")
	}
}

func wave4ClashConfig(t *testing.T, outbounds []map[string]any, template string, policy ClashUDPPolicy) map[string]any {
	t.Helper()
	raw, err := RenderClash(outbounds, template, policy)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestClashWave4ECHKeepsFullPublicContent(t *testing.T) {
	for _, value := range []any{nil, "one-line-config", []string{"one-line-config"}, []any{"-----BEGIN ECH CONFIGS-----", "AA==", "-----END ECH CONFIGS-----"}, "-----BEGIN ECH CONFIGS-----\nAA==\n-----END ECH CONFIGS-----"} {
		outbound := map[string]any{"type": "trojan", "tag": "node", "server": "[2001:db8::1]", "server_port": 443, "password": "fixture", "tls": map[string]any{"enabled": true, "ech": map[string]any{"enabled": true, "config": value}}}
		parsed := wave4ClashConfig(t, []map[string]any{outbound}, DefaultClashConfig, ClashUDPDefault)
		proxy := parsed["proxies"].([]any)[0].(map[string]any)
		if proxy["server"] != "2001:db8::1" {
			t.Fatal("YAML host contains URI brackets or embedded quotes")
		}
		want := ""
		switch config := value.(type) {
		case string:
			want = config
		case []string:
			want = strings.Join(config, "\n")
		case []any:
			for i, line := range config {
				if i > 0 {
					want += "\n"
				}
				want += line.(string)
			}
		}
		if proxy["ech-opts"].(map[string]any)["config"] != want {
			t.Fatal("public ECH boundaries or lines were lost")
		}
	}
	for _, value := range []any{17, []any{"public", false}, "-----BEGIN ECH KEYS-----\nfixture\n-----END ECH KEYS-----", "-----BEGIN PRIVATE KEY-----\nfixture\n-----END PRIVATE KEY-----"} {
		outbound := map[string]any{"type": "trojan", "tag": "node", "server": "example.com", "server_port": 443, "password": "fixture", "tls": map[string]any{"enabled": true, "ech": map[string]any{"enabled": true, "config": value}}}
		if _, err := RenderClash([]map[string]any{outbound}, DefaultClashConfig); err == nil {
			t.Fatal("malformed/private ECH was exported")
		}
	}
	for _, value := range []string{
		"-----BEGIN ECH CONFIGS-----\n%%%\n-----END ECH CONFIGS-----",
		"-----BEGIN ECH CONFIGS-----\nAA==",
		"-----END ECH CONFIGS-----",
		"-----BEGIN ECH CONFIGS-----\nAA==\n-----END CERTIFICATE-----",
		"-----BEGIN ECH CONFIGS-----\n-----BEGIN ECH CONFIGS-----\nAA==\n-----END ECH CONFIGS-----",
		"-----BEGIN ECH CONFIGS-----\nAA==\n-----END ECH CONFIGS-----\n-----END ECH CONFIGS-----",
	} {
		outbound := map[string]any{"type": "trojan", "tag": "node", "server": "example.com", "server_port": 443, "password": "fixture", "tls": map[string]any{"enabled": true, "ech": map[string]any{"enabled": true, "config": value}}}
		if _, err := RenderClash([]map[string]any{outbound}, DefaultClashConfig); err == nil {
			t.Fatal("malformed public PEM boundaries were exported")
		}
	}
}

func TestClashWave4UDPPoliciesUsePortableProtocolFacts(t *testing.T) {
	for _, policy := range []ClashUDPPolicy{ClashUDPDefault, ClashUDPEnabled, ClashUDPDisabled} {
		for _, tc := range []struct {
			protocol string
			network  any
			version  string
			supports bool
		}{{"vmess", nil, "", true}, {"vless", nil, "", true}, {"trojan", "tcp", "", false}, {"http", nil, "", false}, {"socks", nil, "4a", false}, {"socks", "udp", "5", true}, {"tuic", []string{"tcp", "udp"}, "", true}, {"hysteria2", nil, "", true}} {
			outbound := map[string]any{"type": tc.protocol, "tag": "node", "server": "example.com", "server_port": 443, "network": tc.network, "version": tc.version, "username": "fixture", "password": "fixture", "uuid": "11111111-1111-4111-8111-111111111111"}
			parsed := wave4ClashConfig(t, []map[string]any{outbound}, DefaultClashConfig, policy)
			proxy := parsed["proxies"].([]any)[0].(map[string]any)
			if policy == ClashUDPDefault {
				if _, present := proxy["udp"]; present {
					t.Fatal("absent setting changed legacy defaults")
				}
				continue
			}
			if udp, present := proxy["udp"]; !present || udp != (policy == ClashUDPEnabled && tc.supports) {
				t.Fatalf("UDP policy=%q protocol=%q not preserved", policy, tc.protocol)
			}
		}
	}
	if _, err := ParseClashUDPPolicy("1"); err == nil {
		t.Fatal("ambiguous policy accepted")
	}
}

func TestClashWave4CollisionsPreserveNodesAndGroupReferences(t *testing.T) {
	input := []map[string]any{{"type": "trojan", "tag": "node", "server": "a.example", "server_port": 443, "password": "a"}, {"type": "trojan", "tag": "node", "server": "b.example", "server_port": 443, "password": "b"}, {"type": "trojan", "tag": "node-2", "server": "c.example", "server_port": 443, "password": "c"}, {"type": "trojan", "tag": "Proxy", "server": "d.example", "server_port": 443, "password": "d"}, {"type": "selector", "tag": "node", "outbounds": []string{"node", "node-2", "Proxy"}}}
	template := DefaultClashConfig + "\nproxies:\n  - name: Existing\n    type: http\n    server: fixture.example\n    port: 8080\nproxy-groups:\n  - name: node-3\n    type: select\n    proxies: [Existing]\n"
	before, _ := json.Marshal(input)
	parsed := wave4ClashConfig(t, input, template, ClashUDPDefault)
	proxies := parsed["proxies"].([]any)
	var names []string
	for _, proxy := range proxies {
		names = append(names, proxy.(map[string]any)["name"].(string))
	}
	if !reflect.DeepEqual(names, []string{"Existing", "node", "node-4", "node-2", "Proxy-2"}) {
		t.Fatalf("names=%v", names)
	}
	group := clashGroupByName(t, parsed["proxy-groups"].([]any), "node-5")
	if !reflect.DeepEqual(group["proxies"], []any{"node", "node-2", "Proxy-2"}) {
		t.Fatal("group references no longer address their original nodes")
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("Clash projection mutated the canonical input")
	}
	repeated := wave4ClashConfig(t, input, template, ClashUDPDefault)
	if !reflect.DeepEqual(parsed, repeated) {
		t.Fatal("repeated Clash projection changed names")
	}
}
