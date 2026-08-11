package coreinboundcontrol

import (
	"reflect"
	"testing"

	"github.com/sagernet/sing-box/option"
)

func TestPinnedOptionShapesExposeOnlyReviewedFallbackPrimitives(t *testing.T) {
	vless := reflect.TypeFor[option.VLESSInboundOptions]()
	if _, found := vless.FieldByName("Fallback"); found {
		t.Fatal("VLESS unexpectedly exposes a generic fallback")
	}
	for _, field := range []string{"TLS", "Transport", "Multiplex"} {
		if _, found := vless.FieldByName(field); !found {
			t.Fatalf("VLESS option shape lost %s", field)
		}
	}
	trojan := reflect.TypeFor[option.TrojanInboundOptions]()
	for _, field := range []string{"Fallback", "FallbackForALPN", "TLS", "Transport", "Multiplex"} {
		if _, found := trojan.FieldByName(field); !found {
			t.Fatalf("Trojan option shape lost %s", field)
		}
	}
	reality := reflect.TypeFor[option.InboundRealityHandshakeOptions]()
	if _, found := reality.FieldByName("ServerOptions"); !found {
		t.Fatal("REALITY handshake lost TCP server/server_port options")
	}
}
