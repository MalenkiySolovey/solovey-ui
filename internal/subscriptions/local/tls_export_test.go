package local

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestPublicTLSExportBlocksPrivateMaterialAtFinalAddressMerge(t *testing.T) {
	for _, fields := range []string{`"client_key":["private-canary"]`, `"client_key_path":"/private-canary"`, `"certificate_path":"/internal-canary"`, `"certificate_provider":"private-provider"`} {
		inbound := &model.Inbound{Type: "trojan", Tag: "fixture", Options: json.RawMessage(`{}`), OutJson: json.RawMessage(`{"type":"trojan","tag":"fixture","server":"fixture.invalid","server_port":443,"tls":{"enabled":true,"insecure":false}}`), Addrs: json.RawMessage(`[{"server":"fixture.invalid","server_port":443,"tls":{` + fields + `}}]`)}
		set, err := BuildInboundOutbounds(json.RawMessage(`{"trojan":{"password":"delivery-contract"}}`), []*model.Inbound{inbound})
		if err == nil || set != nil || !strings.Contains(err.Error(), "TLS_PRIVATE_EXPORT_REQUIRED") || strings.Contains(err.Error(), "canary") {
			t.Fatal("public export leaked private material or its diagnostic")
		}
	}
	inbound := &model.Inbound{Type: "trojan", Tag: "fixture", Options: json.RawMessage(`{}`), OutJson: json.RawMessage(`{"type":"trojan","tag":"fixture","server":"fixture.invalid","server_port":443,"tls":{"enabled":true,"insecure":false,"certificate_public_key_sha256":["public-pin"]}}`), Addrs: json.RawMessage(`[]`)}
	set, err := BuildInboundOutbounds(json.RawMessage(`{"trojan":{"password":"delivery-contract"}}`), []*model.Inbound{inbound})
	if err != nil || len(set.Outbounds) != 1 || set.Outbounds[0]["password"] != "delivery-contract" {
		t.Fatal("public certificate facts or authorized user delivery regressed")
	}
}
