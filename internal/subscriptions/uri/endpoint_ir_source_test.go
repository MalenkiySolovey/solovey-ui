package uri

import (
	"encoding/base64"
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"testing"
)

func TestLinkCriticalFieldsCannotBeIgnoredByAnotherProtocol(t *testing.T) {
	for _, link := range []string{
		"mierus://fixture:fixture@example.org?profile=node&port=443&protocol=TCP&unmapped=critical",
		"trojan://fixture@example.org:443?security=tls&flow=xtls-rprx-vision",
		"anytls://fixture@example.org:443?type=ws&path=/critical",
		"vless://00000000-0000-4000-8000-000000000001@example.org:443?type=grpc&service_name=critical",
		"vless://00000000-0000-4000-8000-000000000001@example.org:443?security=tls&skip-cert-verify=true",
		"vless://00000000-0000-4000-8000-000000000001@example.org:443?security=tls&insecure=TRUE",
	} {
		node, _, err := Parse(link, 0)
		if err != nil || node == nil {
			t.Fatalf("source was not preserved: %v", err)
		}
		endpoint := canonical.ObserveOutbound(canonical.FormatURI, *node, 0)
		if endpoint.Projection != canonical.ProjectionManual {
			t.Fatal("an ignored critical link field became a default")
		}
		if len(endpoint.Observations) != 1 || endpoint.Observations[0].Source["options"].(map[string]any)["uri"] != link {
			t.Fatal("original link was lost")
		}
		if _, err := canonical.CheckedEndpointOutbound(endpoint); err == nil {
			t.Fatal("ignored critical link field became executable")
		}
	}
}

func TestVMessNativeCriticalCarrierCannotBecomeDefault(t *testing.T) {
	for _, field := range []string{"allowInsecure", "scy"} {
		raw := map[string]any{"v": "2", "ps": "native", "add": "example.org", "port": 443, "id": "00000000-0000-4000-8000-000000000001", "net": "tcp", "tls": "tls"}
		raw[field] = map[string]any{"private-carrier": true}
		body, _ := json.Marshal(raw)
		link := "vmess://" + base64.RawStdEncoding.EncodeToString(body)
		node, _, err := Parse(link, 0)
		if err != nil || node == nil {
			t.Fatalf("native VMess lost: %v", err)
		}
		endpoint := canonical.ObserveOutbound(canonical.FormatURI, *node, 0)
		if endpoint.Projection != canonical.ProjectionManual {
			t.Fatal("VMess carrier became a default")
		}
		if _, err := canonical.CheckedEndpointOutbound(endpoint); err == nil {
			t.Fatal("unmapped VMess carrier became executable")
		}
	}
}
