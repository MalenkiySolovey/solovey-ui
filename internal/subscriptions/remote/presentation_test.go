package remote

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func TestHydrateConnectionTypeInfoSeparatesSourceAndConvertedTypes(t *testing.T) {
	canonicalData, err := json.Marshal(subcanonical.Connection{
		Protocol: "urltest",
		Adaptations: []subcanonical.Adaptation{{
			SourceFormat:  subcanonical.FormatClash,
			SourceFeature: "proxy-groups",
			SourceType:    "load-balance",
			TargetType:    "urltest",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	subscriptions := []model.RemoteOutboundSubscription{{
		Connections: []model.RemoteOutboundConnection{{
			Type:      "urltest",
			Canonical: canonicalData,
		}},
	}}

	HydrateConnectionTypeInfo(subscriptions)

	connection := subscriptions[0].Connections[0]
	if connection.SourceType != "mihomo load-balance" || connection.ConvertedType != "urltest" {
		t.Fatalf("type info = source %q converted %q", connection.SourceType, connection.ConvertedType)
	}
}

func TestHydrateConnectionTypeInfoFallsBackToRuntimeType(t *testing.T) {
	subscriptions := []model.RemoteOutboundSubscription{{
		Connections: []model.RemoteOutboundConnection{{
			Type: "vless",
		}},
	}}

	HydrateConnectionTypeInfo(subscriptions)

	connection := subscriptions[0].Connections[0]
	if connection.SourceType != "vless" || connection.ConvertedType != "vless" {
		t.Fatalf("type info = source %q converted %q", connection.SourceType, connection.ConvertedType)
	}
}
