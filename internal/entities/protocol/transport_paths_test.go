package protocol

import (
	"reflect"
	"testing"
)

func TestInboundClientTransportPathAndIsolation(t *testing.T) {
	for _, kind := range []string{"ws", "http", "httpupgrade"} {
		server := map[string]any{"type": kind, "path": "/literal?token=a%26b#fragment", "max_early_data": 32, "custom": false}
		client := ClientTransportFromInbound(server).(map[string]any)
		literal, err := ClientTransportLiteralPath(client["path"].(string))
		if err != nil || literal != server["path"] {
			t.Fatal("client wire path changed server intent")
		}
		urlPath, err := ClientTransportURLPath(client["path"].(string))
		if err != nil || urlPath != client["path"] {
			t.Fatal("URL consumer path is not stable")
		}
		client["path"] = "changed"
		if server["path"] != "/literal?token=a%26b#fragment" || client["max_early_data"] != 32 || client["custom"] != false {
			t.Fatal("input or unrelated fields mutated")
		}
	}
	for _, value := range []any{nil, 7, map[string]any{"type": "grpc", "path": "/literal%"}, map[string]any{"type": "ws", "path": false}} {
		if !reflect.DeepEqual(ClientTransportFromInbound(value), value) {
			t.Fatal("unrelated or malformed transport was rewritten")
		}
	}
	if _, err := ClientTransportURLPath("/private-token%broken"); err == nil || err.Error() != "invalid client transport path escape" {
		t.Fatal("malformed path did not fail privately")
	}
}
