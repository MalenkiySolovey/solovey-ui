package local

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityprotocol "github.com/MalenkiySolovey/solovey-ui/internal/entities/protocol"
)

func TestOldDerivedTransportPathReadAndExplicitOverride(t *testing.T) {
	config := json.RawMessage(`{"vless":{"uuid":"11111111-1111-4111-8111-111111111111"}}`)
	for _, clientPath := range []string{"/literal?x=a%26b", "/explicit%3Fclient"} {
		out, _ := json.Marshal(map[string]any{"type": "vless", "tag": "fixture", "server": "::1", "server_port": 443, "custom": false, "transport": map[string]any{"type": "ws", "path": clientPath}})
		row := &model.Inbound{Type: "vless", Tag: "fixture", Options: json.RawMessage(`{"transport":{"type":"ws","path":"/literal?x=a%26b"}}`), OutJson: out, Addrs: json.RawMessage(`[]`)}
		before := string(row.OutJson)
		for range 2 {
			set, err := BuildInboundOutbounds(config, []*model.Inbound{row})
			if err != nil || len(set.Outbounds) != 1 {
				t.Fatal("old row export failed", err)
			}
			path := set.Outbounds[0]["transport"].(map[string]any)["path"].(string)
			if clientPath == "/literal?x=a%26b" {
				literal, err := entityprotocol.ClientTransportLiteralPath(path)
				if err != nil || literal != clientPath {
					t.Fatal("old derived path intent changed")
				}
			} else if path != clientPath {
				t.Fatal("explicit client path overwritten")
			}
			if string(row.OutJson) != before || set.Outbounds[0]["custom"] != false {
				t.Fatal("stored row or unrelated state changed")
			}
		}
	}
}
