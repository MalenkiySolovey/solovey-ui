package entityinbounds

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeInboundRejectsAmbiguousOrForeignOwnerInput(t *testing.T) {
	db := newInboundDB(t)
	for _, kind := range []string{"mieru", "trusttunnel", "ssh", "mtproxy"} {
		for _, fields := range []string{
			`"listen_port":23456,"listen_port":23457`,
			`"users":[{"name":"private-name","password":"private-password"}]`,
			`"users":[]`,
			`"tls":{"enabled":true,"key":["private-key"]}`,
		} {
			data := json.RawMessage(`{"type":"` + kind + `","tag":"native",` + fields + `}`)
			_, err := DecodeForSave(db, data)
			if err == nil || !strings.HasPrefix(err.Error(), "NATIVE_INBOUND_") || strings.Contains(err.Error(), "private-") {
				t.Fatal("ambiguous or foreign owner input was accepted or disclosed")
			}
		}
	}
	if _, err := DecodeForSave(db, json.RawMessage(`{"type":"ssh","tag":"native","fallback":{"server":"private-host","server_port":22}}`)); err == nil || !strings.HasPrefix(err.Error(), "NATIVE_INBOUND_OWNED_FIELD") || strings.Contains(err.Error(), "private-host") {
		t.Fatal("SSH fallback acquired a second authentication owner")
	}
}

func TestNativePreservationComparisonRetainsIntegerPrecision(t *testing.T) {
	if sameNativeJSON(json.RawMessage(`{"future":9007199254740992}`), json.RawMessage(`{"future":9007199254740993}`)) {
		t.Fatal("unavailable fields compared through a lossy floating-point value")
	}
	if !sameNativeJSON(json.RawMessage(`{ "future": {"enabled":false,"counter":9007199254740993} }`), json.RawMessage(`{"future":{"counter":9007199254740993,"enabled":false}}`)) {
		t.Fatal("unchanged structured fields lost preservation")
	}
}
