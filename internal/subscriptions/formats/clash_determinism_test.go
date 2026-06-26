package formats

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func diverseClashOutbounds() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"type": "vless", "tag": "vless-1", "server": "a.example.com", "server_port": 443,
			"uuid": "11111111-1111-4111-8111-111111111111", "flow": "xtls-rprx-vision",
			"tls": map[string]interface{}{
				"enabled": true, "server_name": "a.example.com",
				"reality": map[string]interface{}{"enabled": true, "public_key": "pbk", "short_id": "sid"},
				"utls":    map[string]interface{}{"enabled": true, "fingerprint": "chrome"},
			},
			"transport": map[string]interface{}{"type": "grpc", "service_name": "svc"},
		},
		{
			"type": "vmess", "tag": "vmess-1", "server": "b.example.com", "server_port": 8443,
			"uuid": "22222222-2222-4222-8222-222222222222", "alter_id": float64(0),
			"tls":       map[string]interface{}{"enabled": true, "server_name": "b.example.com"},
			"transport": map[string]interface{}{"type": "ws", "path": "/ws", "headers": map[string]interface{}{"Host": "b.example.com"}},
		},
		{"type": "trojan", "tag": "trojan-1", "server": "c.example.com", "server_port": 443, "password": "p", "tls": map[string]interface{}{"enabled": true}},
		{
			"type": "hysteria2", "tag": "hy2-1", "server": "d.example.com", "server_port": 443, "password": "p",
			"up_mbps": float64(100), "down_mbps": float64(200),
			"obfs": map[string]interface{}{"type": "salamander", "password": "o"},
		},
		{"type": "shadowsocks", "tag": "ss-1", "server": "e.example.com", "server_port": 8388, "method": "aes-128-gcm", "password": "p"},
		{
			"type": "tuic", "tag": "tuic-1", "server": "f.example.com", "server_port": 443,
			"uuid": "33333333-3333-4333-8333-333333333333", "password": "p",
			"congestion_control": "bbr", "udp_relay_mode": "quic",
			"tls": map[string]interface{}{"enabled": true},
		},
	}
}

func TestConvertToClashMetaDeterministic(t *testing.T) {
	firstOut := diverseClashOutbounds()
	first, err := RenderClash(firstOut, DefaultClashConfig)
	if err != nil {
		t.Fatalf("ConvertToClashMeta: %v", err)
	}
	for i := 0; i < 8; i++ {
		nextOut := diverseClashOutbounds()
		next, err := RenderClash(nextOut, DefaultClashConfig)
		if err != nil {
			t.Fatalf("ConvertToClashMeta run %d: %v", i+1, err)
		}
		if next != first {
			t.Fatalf("Clash output non-deterministic on run %d", i+1)
		}
	}
}

func TestConvertToClashMetaCoversAllProxies(t *testing.T) {
	outbounds := diverseClashOutbounds()
	got, err := RenderClash(outbounds, DefaultClashConfig)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err := yaml.Unmarshal([]byte(got), &config); err != nil {
		t.Fatalf("output is not valid YAML: %v", err)
	}
	proxies, ok := config["proxies"].([]interface{})
	if !ok || len(proxies) != len(outbounds) {
		t.Fatalf("expected %d proxies, got %#v", len(outbounds), config["proxies"])
	}
	names := map[string]bool{}
	for _, proxyRaw := range proxies {
		proxy, ok := proxyRaw.(map[string]interface{})
		if !ok {
			t.Fatalf("proxy is not a map: %#v", proxyRaw)
		}
		name, ok := proxy["name"].(string)
		if !ok || name == "" {
			t.Fatalf("proxy has no name: %#v", proxy)
		}
		names[name] = true
	}
	for _, outbound := range outbounds {
		tag := outbound["tag"].(string)
		if !names[tag] {
			t.Fatalf("proxy %q missing from rendered proxies", tag)
		}
		if !strings.Contains(got, tag) {
			t.Fatalf("tag %q not referenced anywhere in output", tag)
		}
	}
}
