package formats

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/parser"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri"
)

func TestEndpointIRRepresentativeSemanticRoundTrips(t *testing.T) {
	for _, protocol := range []string{"vless", "vmess", "trojan", "anytls"} {
		t.Run(protocol, func(t *testing.T) {
			raw := map[string]any{"type": protocol, "tag": "Public name", "server": "example.com", "server_port": 443, "tls": map[string]any{"enabled": true, "server_name": "tls.example", "insecure": true, "alpn": []string{"h2", "http/1.1"}, "utls": map[string]any{"enabled": true, "fingerprint": "chrome"}}}
			switch protocol {
			case "vless":
				raw["uuid"] = "11111111-1111-4111-8111-111111111111"
				raw["transport"] = map[string]any{"type": "ws", "path": "/edge", "headers": map[string]any{"Host": "edge.example"}}
			case "vmess":
				raw["uuid"] = "11111111-1111-4111-8111-111111111111"
				raw["security"] = "chacha20-poly1305"
				raw["alter_id"] = 1
				raw["transport"] = map[string]any{"type": "grpc", "service_name": "service"}
			case "trojan":
				raw["password"] = "fixture:/?#"
				raw["transport"] = map[string]any{"type": "ws", "path": "/edge", "headers": map[string]any{"Host": "edge.example"}}
			case "anytls":
				raw["password"] = "fixture:/?#"
			}
			want := canonical.ObserveOutbound(canonical.FormatSingBox, raw, 0)
			check := func(format string, parsed []map[string]any, err error) {
				t.Helper()
				if err != nil || len(parsed) != 1 {
					t.Fatalf("%s parse count=%d err=%v", format, len(parsed), err)
				}
				got := canonical.ObserveOutbound(format, parsed[0], 0)
				if got.Projection != canonical.ProjectionExact {
					t.Fatalf("%s projection=%s diagnostics=%v", format, got.Projection, got.Diagnostics)
				}
				if got.EndpointID != want.EndpointID {
					t.Fatalf("%s changed authentication, TLS or transport facts: want=%s got=%s; fixture before=%#v after=%#v", format, want.EndpointID, got.EndpointID, raw, canonical.CleanOutbound(parsed[0]))
				}
			}
			clash, err := RenderClash([]map[string]any{raw}, DefaultClashConfig)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := parser.ParseClashOutbounds(clash)
			// RenderClash also includes the existing default proxy groups.
			filtered := parsed[:0]
			for _, outbound := range parsed {
				if outbound["type"] == protocol {
					filtered = append(filtered, outbound)
				}
			}
			check(canonical.FormatClash, filtered, err)
			links, err := uri.RenderEndpoint(want)
			if err != nil || len(links) != 1 {
				t.Fatalf("URI render failed: %v", err)
			}
			outbound, _, err := uri.Parse(links[0], 0)
			if err != nil || outbound == nil {
				t.Fatalf("URI parse failed: %v", err)
			}
			check(canonical.FormatURI, []map[string]any{*outbound}, nil)
			if protocol == "anytls" {
				if _, err := RenderXray([]map[string]any{raw}); err == nil {
					t.Fatal("unsupported AnyTLS was omitted or changed to another protocol")
				}
			} else {
				xray, err := RenderXray([]map[string]any{raw})
				if err != nil {
					t.Fatal(err)
				}
				parsed, err := parser.ParseXrayOutbounds(xray)
				filtered := parsed[:0]
				for _, outbound := range parsed {
					if outbound["type"] == protocol {
						filtered = append(filtered, outbound)
					}
				}
				check(canonical.FormatXray, filtered, err)
			}
		})
	}
}

func TestEndpointIRMihomoHTTPUpgradeRetainsTransport(t *testing.T) {
	raw := map[string]any{"type": "vless", "tag": "Upgrade", "server": "example.org", "server_port": 443, "uuid": "00000000-0000-4000-8000-000000000001",
		"tls":       map[string]any{"enabled": true, "server_name": "edge.example.org"},
		"transport": map[string]any{"type": "httpupgrade", "path": "/upgrade", "host": "edge.example.org"}}
	before := canonical.ObserveOutbound(canonical.FormatSingBox, raw, 0)
	body, err := RenderClash([]map[string]any{raw}, DefaultClashConfig)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseClashOutbounds(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range parsed {
		if node["type"] != "vless" {
			continue
		}
		after := canonical.ObserveOutbound(canonical.FormatClash, node, 0)
		if after.Projection != canonical.ProjectionExact || after.EndpointID != before.EndpointID {
			t.Fatal("Mihomo changed HTTPUpgrade into WebSocket or lost its Host")
		}
		return
	}
	t.Fatal("Mihomo endpoint was lost")
}

func TestEndpointIRVLESSRealityRoundTrips(t *testing.T) {
	raw := map[string]any{"type": "vless", "tag": "Reality", "server": "example.org", "server_port": 443, "uuid": "00000000-0000-4000-8000-000000000001", "flow": "xtls-rprx-vision",
		"tls": map[string]any{"enabled": true, "server_name": "edge.example.org", "utls": map[string]any{"enabled": true, "fingerprint": "chrome"}, "reality": map[string]any{"enabled": true, "public_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "short_id": "0123456789abcdef"}}}
	before := canonical.ObserveOutbound(canonical.FormatSingBox, raw, 0)
	check := func(t *testing.T, format string, nodes []map[string]any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range nodes {
			if node["type"] != "vless" {
				continue
			}
			after := canonical.ObserveOutbound(format, node, 0)
			if after.Projection != canonical.ProjectionExact || after.EndpointID != before.EndpointID {
				t.Fatalf("%s changed Reality/authentication facts", format)
			}
			return
		}
		t.Fatalf("%s lost Reality endpoint", format)
	}
	mihomo, err := RenderClash([]map[string]any{raw}, DefaultClashConfig)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := parser.ParseClashOutbounds(mihomo)
	check(t, canonical.FormatClash, nodes, err)
	xray, err := RenderXray([]map[string]any{raw})
	if err != nil {
		t.Fatal(err)
	}
	nodes, err = parser.ParseXrayOutbounds(xray)
	check(t, canonical.FormatXray, nodes, err)
	links, err := uri.RenderEndpoint(before)
	if err != nil || len(links) != 1 {
		t.Fatalf("Reality URI render: %v", err)
	}
	node, _, err := uri.Parse(links[0], 0)
	if node == nil {
		t.Fatalf("Reality URI parse: %v", err)
	}
	check(t, canonical.FormatURI, []map[string]any{*node}, err)
}

func TestEndpointIRPreservesNativeHysteriaObfuscationCarrier(t *testing.T) {
	raw := map[string]any{"type": "hysteria", "tag": "Native obfs", "server": "example.org", "server_port": 443, "auth_str": "fixture-auth", "obfs": "fixture-obfuscation", "tls": map[string]any{"enabled": true, "server_name": "example.org"}}
	before := canonical.ObserveOutbound(canonical.FormatSingBox, raw, 0)
	if _, err := canonical.CheckedEndpointOutbound(before); err != nil {
		t.Fatal("valid native obfuscation carrier was blocked")
	}
	body, err := RenderClash([]map[string]any{raw}, DefaultClashConfig)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := parser.ParseClashOutbounds(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if node["type"] != "hysteria" {
			continue
		}
		after := canonical.ObserveOutbound(canonical.FormatClash, node, 0)
		if after.Projection != canonical.ProjectionExact || after.EndpointID != before.EndpointID {
			t.Fatal("Mihomo lost native obfuscation/authentication")
		}
		return
	}
	t.Fatal("obfuscated endpoint disappeared")
}

func TestEndpointIRDisabledTLSFeaturesCannotBeActivatedBySerialization(t *testing.T) {
	raw := map[string]any{"type": "vless", "tag": "Disabled options", "server": "example.org", "server_port": 443, "uuid": "00000000-0000-4000-8000-000000000001",
		"tls": map[string]any{"enabled": true, "server_name": "example.org", "utls": map[string]any{"enabled": false, "fingerprint": "chrome"}, "reality": map[string]any{"enabled": false, "public_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "short_id": "aabb"}}}
	before := canonical.ObserveOutbound(canonical.FormatSingBox, raw, 0)
	check := func(t *testing.T, format string, nodes []map[string]any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range nodes {
			if node["type"] != "vless" {
				continue
			}
			tls, _ := node["tls"].(map[string]any)
			if tls["reality"] != nil || tls["utls"] != nil {
				t.Fatalf("%s enabled an explicitly disabled TLS feature", format)
			}
			if after := canonical.ObserveOutbound(format, node, 0); after.EndpointID != before.EndpointID {
				t.Fatalf("%s changed identity while dropping inactive TLS settings", format)
			}
			return
		}
		t.Fatalf("%s lost endpoint", format)
	}
	t.Run("Mihomo", func(t *testing.T) {
		body, err := RenderClash([]map[string]any{raw}, DefaultClashConfig)
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := parser.ParseClashOutbounds(body)
		check(t, canonical.FormatClash, nodes, err)
	})
	t.Run("Xray", func(t *testing.T) {
		body, err := RenderXray([]map[string]any{raw})
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := parser.ParseXrayOutbounds(body)
		check(t, canonical.FormatXray, nodes, err)
	})
	t.Run("URI", func(t *testing.T) {
		links, err := uri.RenderEndpoint(before)
		if err != nil || len(links) != 1 {
			t.Fatalf("URI render: %v", err)
		}
		node, _, err := uri.Parse(links[0], 0)
		if node == nil {
			t.Fatalf("URI parse: %v", err)
		}
		check(t, canonical.FormatURI, []map[string]any{*node}, err)
	})
}

func TestEndpointIRMalformedSOCKSVersionCannotBecomeSOCKS5(t *testing.T) {
	for _, version := range []any{false, map[string]any{"private": "version-marker"}} {
		raw := map[string]any{"type": "socks", "tag": "Malformed version", "server": "example.org", "server_port": 1080, "version": version}
		if _, err := RenderClash([]map[string]any{raw}, DefaultClashConfig); err == nil {
			t.Fatal("Mihomo silently replaced malformed SOCKS version with SOCKS5")
		}
		if _, err := RenderXray([]map[string]any{raw}); err == nil {
			t.Fatal("Xray silently replaced malformed SOCKS version with SOCKS5")
		}
	}
}

func TestEndpointIRCriticalUnsupportedProjectionIsExplicit(t *testing.T) {
	raw := map[string]any{"type": "vless", "tag": "fixture", "server": "example.com", "server_port": 443, "uuid": "11111111-1111-4111-8111-111111111111", "transport": map[string]any{"type": "ws", "headers": map[string]any{"X-Critical": "never-expose-this-value"}}}
	endpoint := canonical.ObserveOutbound(canonical.FormatSingBox, raw, 0)
	_, err := uri.RenderEndpoint(endpoint)
	if err == nil || !strings.Contains(err.Error(), "UNSUPPORTED") || strings.Contains(err.Error(), "never-expose") {
		t.Fatal("URI transport loss was accepted or leaked source values")
	}
	raw["transport"] = map[string]any{"type": "future-transport", "mode": "required"}
	if _, err := RenderClash([]map[string]any{raw}, DefaultClashConfig); err == nil {
		t.Fatal("unsupported transport silently became TCP")
	}
}
