package local_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/formats"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/local"
	"github.com/sagernet/sing-box/option"
	sbjson "github.com/sagernet/sing/common/json"
)

func TestSnellJSONExportUsesPerClientCredentialsAndPinnedSchema(t *testing.T) {
	for _, version := range []int{4, 6} {
		inbound := &model.Inbound{Type: "snell", Tag: "snell", Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(fmt.Sprintf(`{"type":"snell","tag":"snell","version":%d,"psk":"fixture-psk-12","server":"example.invalid","server_port":443}`, version))}
		set, err := local.BuildInboundOutbounds(json.RawMessage(`{"snell":{"name":"private-label","userkey":"fixture-client-key"}}`), []*model.Inbound{inbound})
		if err != nil || len(set.Outbounds) != 1 || set.Outbounds[0]["userkey"] != "fixture-client-key" {
			t.Fatal("client key not exported", err)
		}
		result, err := formats.RenderJSON(set.Outbounds, formats.JSONOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var config option.Options
		if err := sbjson.UnmarshalContext(registry.Context(t.Context()), []byte(result), &config); err != nil {
			t.Fatal("pinned core rejected JSON export", err)
		}
		if strings.Contains(result, "private-label") {
			t.Fatal("runtime principal leaked into public label")
		}
		if _, err := local.BuildInboundOutbounds(json.RawMessage(`{}`), []*model.Inbound{inbound}); err == nil {
			t.Fatal("PSK-only client export accepted")
		}
	}
	if clientfacts.CanDeliver("snell", "uri") {
		t.Fatal("invented Snell share URI")
	}
}
