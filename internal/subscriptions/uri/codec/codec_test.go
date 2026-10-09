package codec

import (
	"encoding/base64"
	"testing"
)

func TestHostNormalizationUsesIPSemantics(t *testing.T) {
	for _, host := range []string{"192.0.2.1", "node.example", "2001:db8::1", "[2001:db8::1]", "fe80::1%fixture0"} {
		normalized, err := NormalizeHost(host)
		if err != nil {
			t.Fatal(err)
		}
		if host == "[2001:db8::1]" && normalized != "2001:db8::1" {
			t.Fatal("JSON host retained brackets")
		}
		if _, err := Authority(host, 443); err != nil {
			t.Fatal(err)
		}
	}
	for _, host := range []string{"", " node.example", "node.example:443", "[node.example]", "[2001:db8::1", "2001:db8::1]", "[[2001:db8::1]]", "[2001:db8::1]:443", "bad/host", "bad host", "bad@host"} {
		if _, err := NormalizeHost(host); err == nil {
			t.Fatalf("malformed host accepted: %q", host)
		}
	}
	if host, err := NormalizeHost("2001:db8::1:443"); err != nil || host != "2001:db8::1:443" {
		t.Fatal("a valid raw IPv6 address must not be guessed as a host-port pair")
	}
}

func TestDecodeAcceptsLegacyAndURLBase64(t *testing.T) {
	value := []byte("credential /?%雪")
	for _, encoder := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		decoded, err := Decode(encoder.EncodeToString(value))
		if err != nil || string(decoded) != string(value) {
			t.Fatal("base64 compatibility lost")
		}
	}
	if _, err := Decode("%%%notbase64"); err == nil {
		t.Fatal("malformed base64 accepted")
	}
}

func TestWebSocketQueriesAndBounds(t *testing.T) {
	for _, transport := range []map[string]any{{"path": 17}, {"early_data_header_name": true}} {
		if _, err := WebSocketPath(transport); err == nil {
			t.Fatal("invalid WebSocket text metadata accepted")
		}
	}
	for _, transport := range []map[string]any{{"max_early_data": -1}, {"max_early_data": float64(1 << 32)}, {"max_early_data": 1.5}, {"max_early_data": "2048"}, {"path": "/?ed=0"}, {"path": "/?ed=1&ed=2"}, {"path": "/?token=%zz"}, {"path": "/?ed=1024", "max_early_data": 2048, "early_data_header_name": WebSocketProtocolHeader}} {
		if _, err := WebSocketPath(transport); err == nil {
			t.Fatal("malformed early-data state accepted")
		}
	}
	path, err := WebSocketPath(map[string]any{"path": "/?token=a%26b&", "max_early_data": 2048, "early_data_header_name": "sec-websocket-protocol"})
	if err != nil || path != "/?token=a%26b&ed=2048" {
		t.Fatal("existing path query was not preserved")
	}
}
