package canonical

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEndpointProjectionDoesNotInventPolicy(t *testing.T) {
	endpoint := map[string]any{"type": "vless", "tag": "vpn", "server": "edge.example", "server_port": 443,
		"uuid": "11111111-1111-4111-8111-111111111111"}
	for _, target := range []string{FormatSingBox, FormatClash, FormatXray, FormatURI} {
		t.Run(target, func(t *testing.T) {
			projected, err := ProjectOutbounds(target, []map[string]any{endpoint})
			if err != nil || len(projected) != 1 || projected[0]["type"] != "vless" {
				t.Fatalf("endpoint became a policy or fallback: %v", err)
			}
			for _, protocol := range []string{"selector", "urltest", "failover", "direct", "block"} {
				policy := map[string]any{"type": protocol, "tag": "declared"}
				if protocol != "direct" && protocol != "block" {
					policy["outbounds"] = []any{"vpn"}
				}
				before, err := json.Marshal(policy)
				if err != nil {
					t.Fatal(err)
				}
				result, err := ProjectOutbounds(target, []map[string]any{policy})
				if err != nil || len(result) != 1 {
					t.Fatalf("declared policy %s: %v", protocol, err)
				}
				if result[0]["type"] != protocol || !reflect.DeepEqual(result[0]["outbounds"], policy["outbounds"]) {
					t.Fatalf("declared %s policy changed", protocol)
				}
				for _, key := range []string{"final", "default", "fallback", "fallbackTag"} {
					if _, present := result[0][key]; present {
						t.Fatalf("invented %s for %s", key, protocol)
					}
				}
				after, err := json.Marshal(policy)
				if err != nil || string(before) != string(after) {
					t.Fatal("input policy mutated", err)
				}
			}
			invalid := CloneOutbound(endpoint)
			invalid["tls"] = "unrepresentable-security"
			if result, err := ProjectOutbounds(target, []map[string]any{invalid}); err == nil || result != nil {
				t.Fatal("unsupported security produced a profile or fallback")
			}
		})
	}
}
