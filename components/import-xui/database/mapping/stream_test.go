//go:build !minimal

package mapping

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/components/import-xui/database/source"
)

func TestParseStreamSettingsNormalizesInboundAndOutbound(t *testing.T) {
	inbound, err := parseStreamSettings(source.InboundRow{
		Tag:            "in",
		StreamSettings: json.RawMessage(`{"network":" WS ","security":" TLS "}`),
	})
	if err != nil {
		t.Fatalf("parse inbound stream: %v", err)
	}
	if inbound.Network != "ws" || inbound.Security != "tls" {
		t.Fatalf("inbound stream = %q/%q, want ws/tls", inbound.Network, inbound.Security)
	}

	outbound, err := parseOutboundStream(xrayOutbound{
		Tag:            "out",
		StreamSettings: json.RawMessage(`{"network":" GRPC ","security":" REALITY "}`),
	})
	if err != nil {
		t.Fatalf("parse outbound stream: %v", err)
	}
	if outbound.Network != "grpc" || outbound.Security != "reality" {
		t.Fatalf("outbound stream = %q/%q, want grpc/reality", outbound.Network, outbound.Security)
	}
}

func TestParseOutboundStreamRejectsInvalidJSON(t *testing.T) {
	if _, err := parseOutboundStream(xrayOutbound{
		Tag:            "bad",
		StreamSettings: json.RawMessage(`{`),
	}); err == nil {
		t.Fatal("invalid outbound stream was accepted")
	}
}
