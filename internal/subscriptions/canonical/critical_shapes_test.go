package canonical

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEndpointIRPreservesAndBlocksMalformedCriticalCarriers(t *testing.T) {
	const marker = "private-shape-fixture"
	for name, field := range map[string]map[string]any{
		"authentication":     {"uuid": map[string]any{"private": marker}},
		"tls object":         {"tls": marker},
		"tls flag":           {"tls": map[string]any{"enabled": marker}},
		"tls server name":    {"tls": map[string]any{"enabled": true, "server_name": map[string]any{"private": marker}}},
		"reality public key": {"tls": map[string]any{"enabled": true, "reality": map[string]any{"enabled": true, "public_key": map[string]any{"private": marker}}}},
		"fingerprint":        {"tls": map[string]any{"enabled": true, "utls": map[string]any{"enabled": true, "fingerprint": map[string]any{"private": marker}}}},
		"ECH config":         {"tls": map[string]any{"enabled": true, "ech": map[string]any{"enabled": true, "config": map[string]any{"private": marker}}}},
		"tls nested":         {"tls": map[string]any{"enabled": true, "reality": marker}},
		"tls list":           {"tls": map[string]any{"enabled": true, "alpn": []any{marker, 42}}},
		"transport object":   {"transport": marker},
		"transport headers":  {"transport": map[string]any{"type": "ws", "headers": marker}},
		"transport path":     {"transport": map[string]any{"type": "ws", "path": map[string]any{"private": marker}}},
		"transport host":     {"transport": map[string]any{"type": "httpupgrade", "host": map[string]any{"private": marker}}},
		"multiplex":          {"multiplex": map[string]any{"enabled": marker}},
		"obfuscation":        {"obfs": marker},
	} {
		t.Run(name, func(t *testing.T) {
			raw := map[string]any{"type": "vless", "server": "example.org", "server_port": 443, "uuid": "fixture-uuid"}
			for key, value := range field {
				raw[key] = value
			}
			endpoint := ObserveOutbound(FormatSingBox, raw, 0)
			if endpoint.Projection != ProjectionManual || len(endpoint.Diagnostics) == 0 {
				t.Fatal("malformed field was accepted")
			}
			if _, err := CheckedEndpointOutbound(endpoint); err == nil {
				t.Fatal("malformed endpoint became active")
			}
			encoded, err := json.Marshal(ConnectionOutbound(endpoint))
			if err != nil || !strings.Contains(string(encoded), marker) {
				t.Fatal("read projection lost the captured critical field")
			}
			diagnostics, _ := json.Marshal(endpoint.Diagnostics)
			if strings.Contains(string(diagnostics), marker) {
				t.Fatal("diagnostic exposed a private value")
			}
		})
	}
}

func TestEndpointIRAnyTLSRealityCannotLoseRealityInMihomo(t *testing.T) {
	raw := map[string]any{"type": "anytls", "server": "example.org", "server_port": 443, "password": "private-fixture",
		"tls": map[string]any{"enabled": true, "server_name": "example.org", "reality": map[string]any{"enabled": true, "public_key": "fixture-key", "short_id": "aabb"}}}
	endpoint := ObserveOutbound(FormatSingBox, raw, 0)
	if err := ValidateEndpointProjection(endpoint, FormatClash); err == nil {
		t.Fatal("AnyTLS Reality was accepted by a target that cannot preserve it")
	}
	if _, err := CheckedEndpointOutbound(endpoint); err != nil {
		t.Fatal("target-specific incompatibility changed the source endpoint")
	}
	if endpoint.TLS.Options["reality"] == nil {
		t.Fatal("source Reality was discarded")
	}
}
