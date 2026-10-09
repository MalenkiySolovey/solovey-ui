package formats

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/parser"
	"github.com/sagernet/sing-box/option"
	"gopkg.in/yaml.v3"
)

func TestClashWave4WSAndECHRoundTripUsesOfficialOptions(t *testing.T) {
	outbound := map[string]any{"type": "vless", "tag": "node", "server": "2001:db8::1", "server_port": 443, "uuid": "11111111-1111-4111-8111-111111111111", "transport": map[string]any{"type": "ws", "path": "/雪?token=a%26b", "max_early_data": 2048, "early_data_header_name": "X-Custom"}, "tls": map[string]any{"enabled": true, "ech": map[string]any{"enabled": true, "config": []string{"-----BEGIN ECH CONFIGS-----", "AA==", "-----END ECH CONFIGS-----"}}}}
	rendered, err := RenderClash([]map[string]any{outbound}, DefaultClashConfig)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseClashOutbounds(rendered)
	if err != nil {
		t.Fatal(err)
	}
	transport := parsed[0]["transport"].(map[string]any)
	if transport["path"] != outbound["transport"].(map[string]any)["path"] || transport["max_early_data"] != uint32(2048) || transport["early_data_header_name"] != "X-Custom" {
		t.Fatal("Clash WS round trip dropped early data or path query")
	}
	raw, _ := json.Marshal(transport)
	var official option.V2RayWebsocketOptions
	if err := json.Unmarshal(raw, &official); err != nil || official.MaxEarlyData != 2048 || official.EarlyDataHeaderName != "X-Custom" {
		t.Fatal("projected WS state does not match the pinned core option contract")
	}
	if parsed[0]["tls"].(map[string]any)["ech"].(map[string]any)["config"].([]string)[0] != "-----BEGIN ECH CONFIGS-----\nAA==\n-----END ECH CONFIGS-----" {
		t.Fatal("Clash ECH import truncated public content")
	}
}

func TestClashWave4PortHoppingRoundTripAndUnsupportedShapes(t *testing.T) {
	outbound := map[string]any{"type": "hysteria2", "tag": "node", "server": "example.com", "server_port": 443, "password": "fixture", "server_ports": []string{"443:443", "8443:8445", "65535:"}, "hop_interval": "30s"}
	rendered, err := RenderClash([]map[string]any{outbound}, DefaultClashConfig)
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := yaml.Unmarshal([]byte(rendered), &output); err != nil {
		t.Fatal(err)
	}
	proxy := output["proxies"].([]any)[0].(map[string]any)
	if proxy["ports"] != "443,8443-8445,65535" || proxy["hop-interval"] != 30 {
		t.Fatal("Clash port grammar or interval seconds changed")
	}
	parsed, err := parser.ParseClashOutbounds(rendered)
	if err != nil {
		t.Fatal(err)
	}
	if parsed[0]["hop_interval"] != "30s" || !reflect.DeepEqual(parsed[0]["server_ports"], []string{"443:443", "8443:8445", "65535:65535"}) {
		t.Fatal("Clash port hopping did not survive import/export")
	}
	outbound["hop_interval"] = "500ms"
	if _, err := RenderClash([]map[string]any{outbound}, DefaultClashConfig); err == nil {
		t.Fatal("unrepresentable interval was silently rounded")
	}
	if _, err := parser.ParseClashOutbounds("proxies:\n  - {type: hysteria2, name: node, server: example.com, port: 443, password: fixture, ports: '2-1'}"); err == nil {
		t.Fatal("malformed imported range accepted")
	}
	if _, err := parser.ParseClashOutbounds("proxies:\n  - {type: hysteria2, name: node, server: example.com, port: 443, password: fixture, hop-interval: '15-30'}"); err == nil {
		t.Fatal("random client interval was silently converted into a fixed interval")
	}
}
