package formats

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/parser"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri"
)

func TestXHTTPRepresentationsKeepEndpointIdentity(t *testing.T) {
	for _, protocol := range []string{"vless", "trojan"} {
		t.Run(protocol, func(t *testing.T) {
			raw := map[string]any{"type": protocol, "tag": "native", "server": "edge.example", "server_port": 443,
				"tls":       map[string]any{"enabled": true, "server_name": "edge.example"},
				"transport": map[string]any{"type": "xhttp", "path": "/literal?ed=2048&key=value", "host": "cdn.example", "mode": "stream-up"}}
			if protocol == "vless" {
				raw["uuid"] = "00000000-0000-4000-8000-000000000001"
			} else {
				raw["password"] = "synthetic-xhttp/credential"
			}
			original := canonical.ObserveOutbound(canonical.FormatSingBox, raw, 0)
			assertSame := func(value map[string]any, format string) {
				t.Helper()
				endpoint := canonical.ObserveOutbound(format, value, 0)
				if _, err := canonical.CheckedEndpointOutbound(endpoint); err != nil || endpoint.EndpointID != original.EndpointID {
					t.Fatalf("native XHTTP semantics changed across %s: %v", format, err)
				}
			}
			links, err := uri.RenderEndpoint(original)
			if err != nil || len(links) != 1 {
				t.Fatalf("native XHTTP URI unavailable: %v", err)
			}
			parsed, _, err := uri.Parse(links[0], 0)
			if err != nil || parsed == nil {
				t.Fatal("native XHTTP URI was not readable", err)
			}
			assertSame(*parsed, canonical.FormatURI)
			alias, _, err := uri.Parse(strings.Replace(links[0], "type=xhttp", "type=splithttp", 1), 0)
			if err != nil || alias == nil {
				t.Fatal("SplitHTTP source alias was lost", err)
			}
			assertSame(*alias, canonical.FormatURI)
			// Arbitrary HTTP headers require JSON; a share link must not drop them.
			raw["transport"].(map[string]any)["headers"] = map[string]any{"X-Fixture": "retained"}
			original = canonical.ObserveOutbound(canonical.FormatSingBox, raw, 0)
			if _, err := uri.RenderEndpoint(original); err == nil {
				t.Fatal("URI silently discarded XHTTP headers")
			}
			xray, err := RenderXray([]map[string]any{raw})
			if err != nil || strings.Contains(xray, "httpupgrade") {
				t.Fatal("XHTTP was substituted during Xray rendering", err)
			}
			outbounds, err := parser.ParseXrayOutbounds(xray)
			if err != nil {
				t.Fatal("Xray XHTTP endpoint was lost", err)
			}
			found := false
			for _, outbound := range outbounds {
				if outbound["type"] == protocol && outbound["server"] == raw["server"] {
					assertSame(outbound, canonical.FormatXray)
					found = true
				}
			}
			if !found {
				t.Fatal("Xray profile lost its native endpoint member")
			}
			if _, err := RenderClash([]map[string]any{raw}, ""); err == nil {
				t.Fatal("unqualified target silently accepted XHTTP")
			}
		})
	}
}
