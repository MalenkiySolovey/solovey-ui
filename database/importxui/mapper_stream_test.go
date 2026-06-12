package importxui

import (
	"encoding/json"
	"testing"
)

func TestParseStreamSettingsNormalizesInboundAndOutbound(t *testing.T) {
	inbound, err := parseStreamSettings(xuiInboundRow{
		Tag:            "in",
		StreamSettings: json.RawMessage(`{"network":" WS ","security":" TLS "}`),
	})
	if err != nil {
		t.Fatalf("parse inbound stream: %v", err)
	}
	if inbound.Network != "ws" || inbound.Security != "tls" {
		t.Fatalf("inbound stream = %q/%q, want ws/tls", inbound.Network, inbound.Security)
	}

	outbound := parseOutboundStream(xrayOutbound{
		Tag:            "out",
		StreamSettings: json.RawMessage(`{"network":" GRPC ","security":" REALITY "}`),
	})
	if outbound.Network != "grpc" || outbound.Security != "reality" {
		t.Fatalf("outbound stream = %q/%q, want grpc/reality", outbound.Network, outbound.Security)
	}
}

func TestParseOutboundStreamInvalidJSONYieldsZeroStream(t *testing.T) {
	stream := parseOutboundStream(xrayOutbound{
		Tag:            "bad",
		StreamSettings: json.RawMessage(`{`),
	})
	if stream.Network != "" || stream.Security != "" {
		t.Fatalf("invalid outbound stream = %q/%q, want zero stream", stream.Network, stream.Security)
	}
}
