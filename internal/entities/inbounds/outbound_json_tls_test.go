package entityinbounds

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestAddTLSInitializesMissingClientRealityAndECH(t *testing.T) {
	tls := &model.Tls{
		Server: json.RawMessage(`{
			"enabled": true,
			"reality": {"enabled": true, "short_id": ["only-id"]},
			"ech": {"enabled": true, "pq_signature_schemes_enabled": true}
		}`),
		Client: json.RawMessage(`{}`),
	}
	out := map[string]interface{}{}

	addTls(&out, tls)

	tlsConfig, ok := out["tls"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing TLS config: %#v", out)
	}
	reality, ok := tlsConfig["reality"].(map[string]interface{})
	if !ok || reality["enabled"] != true || reality["short_id"] != "only-id" {
		t.Fatalf("unexpected Reality client config: %#v", reality)
	}
	ech, ok := tlsConfig["ech"].(map[string]interface{})
	if !ok || ech["enabled"] != true || ech["pq_signature_schemes_enabled"] != true {
		t.Fatalf("unexpected ECH client config: %#v", ech)
	}
}

func TestAddTLSHandlesMalformedOptionalBlocksWithoutPanic(t *testing.T) {
	tests := []struct {
		name   string
		server string
		client string
	}{
		{name: "wrong enabled types", server: `{"reality":{"enabled":"yes"},"ech":{"enabled":1}}`, client: `{}`},
		{name: "null client", server: `{"enabled":true,"reality":{"enabled":true}}`, client: `null`},
		{name: "wrong client blocks", server: `{"reality":{"enabled":true},"ech":{"enabled":true}}`, client: `{"reality":false,"ech":[]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out := map[string]interface{}{}
			addTls(&out, &model.Tls{Server: json.RawMessage(test.server), Client: json.RawMessage(test.client)})
			if _, ok := out["tls"].(map[string]interface{}); !ok {
				t.Fatalf("TLS config was not produced: %#v", out)
			}
		})
	}
}
