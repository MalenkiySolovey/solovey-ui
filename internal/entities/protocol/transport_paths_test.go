package protocol

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestTransportEditorUsesTheSelectedNativeConsumer(t *testing.T) {
	facts := EditorContract().Transports
	if !facts["grpc"].Available {
		t.Fatal("the native lite gRPC implementation was hidden")
	}
	source := json.RawMessage(`{"transport":{"type":"xhttp","mode":"packet-up","x_padding_bytes":"100-1000","path":"/private-path?token=fixture"}}`)
	findings := CurrentFindings("vless", "outbound", "outbound", source)
	if facts["xhttp"].Available {
		if len(facts["xhttp"].Fields["root"]) == 0 || len(findings) != 0 {
			t.Fatal("native XHTTP fields or valid configuration were lost")
		}
		invalid := bytes.Replace(source, []byte(`"mode":"packet-up"`), []byte(`"mode":"stream-one","download":{"x_padding_bytes":"100-1000"}`), 1)
		if len(CurrentFindings("vless", "outbound", "outbound", invalid)) == 0 {
			t.Fatal("incompatible split download mode was accepted")
		}
	} else if len(findings) != 1 || findings[0].Code != "TRANSPORT_UNAVAILABLE" {
		t.Fatal("unavailable transport did not fail explicitly")
	}
	if findings := CurrentFindings("mieru", "outbound", "outbound", json.RawMessage(`{"transport":"TCP"}`)); len(findings) != 0 {
		t.Fatal("native V2Ray schema captured Mieru's independent transport enum")
	}
}

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
