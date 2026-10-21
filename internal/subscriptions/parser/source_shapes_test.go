package parser

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func TestNativeCriticalShapesCannotBecomeParserDefaults(t *testing.T) {
	const xray = `{"tag":"native","protocol":"vless","settings":{"vnext":[{"address":"example.org","port":443,"users":[{"id":"00000000-0000-4000-8000-000000000001"}]}]},"streamSettings":{"security":"tls","network":"ws","tlsSettings":{"serverName":"example.org"},"wsSettings":{"path":"/","headers":{"Host":"edge.example.org"}}}}`
	const clash = `{"name":"native","type":"vless","server":"example.org","port":443,"uuid":"00000000-0000-4000-8000-000000000001","tls":true,"network":"ws","ws-opts":{"path":"/","headers":{"Host":"edge.example.org"}}}`
	for _, tc := range []struct {
		name, format, source string
		mutate               func(map[string]any)
	}{
		{"Xray TLS flag", canonical.FormatXray, xray, func(raw map[string]any) {
			raw["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["allowInsecure"] = "private-shape-marker"
		}},
		{"Xray TLS object", canonical.FormatXray, xray, func(raw map[string]any) {
			raw["streamSettings"].(map[string]any)["tlsSettings"] = "private-shape-marker"
		}},
		{"Xray credential", canonical.FormatXray, xray, func(raw map[string]any) {
			raw["settings"].(map[string]any)["vnext"].([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)["id"] = map[string]any{"private-shape-marker": true}
		}},
		{"Mihomo TLS flag", canonical.FormatClash, clash, func(raw map[string]any) { raw["skip-cert-verify"] = "private-shape-marker" }},
		{"Mihomo headers", canonical.FormatClash, clash, func(raw map[string]any) { raw["ws-opts"].(map[string]any)["headers"] = "private-shape-marker" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var raw map[string]any
			if err := json.Unmarshal([]byte(tc.source), &raw); err != nil {
				t.Fatal(err)
			}
			tc.mutate(raw)
			var nodes []map[string]any
			var err error
			if tc.format == canonical.FormatXray {
				body, _ := json.Marshal(map[string]any{"outbounds": []any{raw}})
				nodes, err = ParseXrayOutbounds(string(body))
			} else {
				body, _ := json.Marshal(map[string]any{"proxies": []any{raw}})
				nodes, err = ParseClashOutbounds(string(body))
			}
			if err != nil || len(nodes) != 1 {
				t.Fatalf("native endpoint was lost: count=%d err=%v", len(nodes), err)
			}
			endpoint := canonical.ObserveOutbound(tc.format, nodes[0], 0)
			if endpoint.Projection != canonical.ProjectionManual {
				t.Fatal("invalid native carrier became an executable default")
			}
			if len(endpoint.Observations) != 1 {
				t.Fatal("native observation is missing")
			}
			preserved, _ := json.Marshal(endpoint.Observations[0].Source["options"])
			original, _ := json.Marshal(raw)
			if string(preserved) != string(original) {
				t.Fatal("original critical source was not preserved")
			}
			if _, err := canonical.CheckedEndpointOutbound(endpoint); err == nil {
				t.Fatal("unmapped native carrier was executable")
			}
			diagnostics, _ := json.Marshal(endpoint.Diagnostics)
			if strings.Contains(string(diagnostics), "private-shape-marker") {
				t.Fatal("diagnostics exposed a source value")
			}
		})
	}
}

func TestIncompleteNativeEndpointCannotDisappearFromSnapshot(t *testing.T) {
	body := `{"outbounds":[{"tag":"valid","protocol":"vless","settings":{"vnext":[{"address":"example.org","port":443,"users":[{"id":"00000000-0000-4000-8000-000000000001"}]}]}},{"tag":"incomplete","protocol":"trojan","settings":{"servers":[]}}]}`
	nodes, err := ParseXrayOutbounds(body)
	if err == nil || len(nodes) != 0 {
		t.Fatalf("partial snapshot accepted: count=%d err=%v", len(nodes), err)
	}
	if nodes, err := ParseClashOutbounds(`{"proxies":[{"name":"valid","type":"vless","server":"example.org","port":443,"uuid":"00000000-0000-4000-8000-000000000001"},"malformed"]}`); err == nil || len(nodes) != 0 {
		t.Fatal("malformed native entry published a partial Mihomo snapshot")
	}
}
