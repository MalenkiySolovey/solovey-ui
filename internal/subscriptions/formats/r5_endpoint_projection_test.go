package formats

import (
	"strings"
	"testing"
)

func TestR5ClashAnyTLSUsesSharedTLSAndExplicitUDPPolicy(t *testing.T) {
	for _, policy := range []ClashUDPPolicy{ClashUDPDefault, ClashUDPEnabled, ClashUDPDisabled} {
		source := map[string]any{"type": "anytls", "tag": "fixture", "server": "example.com", "server_port": 443, "password": "synthetic-fixture", "tls": map[string]any{"enabled": true, "server_name": "sni.example.com"}}
		rendered := wave4ClashConfig(t, []map[string]any{source}, DefaultClashConfig, policy)
		proxy := rendered["proxies"].([]any)[0].(map[string]any)
		if value, exists := proxy["skip-cert-verify"]; exists {
			t.Fatalf("unset insecure became a TLS override: %#v", value)
		}
		if proxy["sni"] != "sni.example.com" {
			t.Fatal("SNI was lost")
		}
		udp, exists := proxy["udp"]
		if policy == ClashUDPDefault && exists || policy != ClashUDPDefault && (!exists || udp != (policy == ClashUDPEnabled)) {
			t.Fatal("explicit UDP policy was not retained")
		}
	}
}

func TestR5ClashUnknownCriticalProtocolFailsVisibly(t *testing.T) {
	_, err := RenderClash([]map[string]any{{"type": "unreviewed-protocol", "tag": "fixture", "server": "example.com", "server_port": 443, "password": "must-not-appear"}}, DefaultClashConfig)
	if err == nil {
		t.Fatal("unknown endpoint silently disappeared from the target profile")
	}
	if strings.Contains(err.Error(), "must-not-appear") {
		t.Fatal("unsupported diagnostic exposed authentication material")
	}
}
