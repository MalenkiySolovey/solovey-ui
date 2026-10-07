package entityinbounds

import (
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"testing"
)

func TestTUNDNSClassificationDoesNotAuthorizeMutation(t *testing.T) {
	for _, mode := range []string{"", "disabled", "native", "hijack"} {
		for _, extras := range []string{"", `,"auto_redirect":true,"strict_route":true`, `,"dns_address":["192.0.2.53"]`} {
			raw := []byte(`{"inbounds":[{"type":"tun","dns_mode":"` + mode + `"` + extras + `}]}`)
			findings := TUNDNSFindings(raw)
			if (diagnostics.FirstError(findings) != nil) != (mode != "disabled") {
				t.Fatalf("incorrect mode authorization: %s", mode)
			}
			if len(findings) > 0 && findings[0].Path != "inbounds[0].dns_mode" {
				t.Fatal("missing owner path")
			}
		}
	}
}

func TestTUNDNSSaveUsesOwnerAuthorization(t *testing.T) {
	_, err := DecodeForSave(nil, json.RawMessage(`{"type":"tun","tag":"tun-test","dns_mode":"native"}`))
	if err == nil {
		t.Fatal("save bypassed interface-DNS authorization")
	}
	_, err = DecodeForSave(nil, json.RawMessage(`{"type":"tun","tag":"tun-test","dns_mode":"disabled"}`))
	if err != nil {
		t.Fatal(err)
	}
}
