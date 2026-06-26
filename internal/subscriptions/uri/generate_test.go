package uri

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestLinkGeneratorTUICIncludesUDPRelayMode(t *testing.T) {
	link := generateTUICLinkForTest(t, `{"udp_relay_mode":"native"}`)
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}

	if got := u.Query().Get("udp_relay_mode"); got != "native" {
		t.Fatalf("expected udp_relay_mode=native, got %q in %s", got, link)
	}
}

func TestLinkGeneratorTUICRoundTripPreservesUDPRelayMode(t *testing.T) {
	link := generateTUICLinkForTest(t, `{"udp_relay_mode":"quic"}`)
	outbound, _, err := Parse(link, 0)
	if err != nil {
		t.Fatal(err)
	}

	if got := (*outbound)["udp_relay_mode"]; got != "quic" {
		t.Fatalf("expected round-trip udp_relay_mode=quic, got %#v", got)
	}
}

func TestLinkGeneratorTUICDefaultsUDPRelayMode(t *testing.T) {
	link := generateTUICLinkForTest(t, `{}`)
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}

	if got := u.Query().Get("udp_relay_mode"); got != defaultTUICUDPRelayMode {
		t.Fatalf("expected default udp_relay_mode=%s, got %q in %s", defaultTUICUDPRelayMode, got, link)
	}
}

// TestLinkGeneratorMalformedAddrsDoesNotPanic feeds an addr map missing
// server/remark and carrying non-bool tls.enabled / non-string alpn elements.
// Before the comma-ok hardening (Q4) these tripped interface-conversion panics
// in the subscription request goroutine; now every link type degrades to a
// partial/empty link instead.
func TestLinkGeneratorMalformedAddrsDoesNotPanic(t *testing.T) {
	malformedAddrs := json.RawMessage(`[
		{"tls":{"enabled":"yes"}},
		{"tls":{"enabled":true,"alpn":[123],"reality":{"enabled":"yes"}}}
	]`)
	clientConfig := json.RawMessage(`{
		"vless": {"uuid":"11111111-1111-4111-8111-111111111111","flow":"xtls-rprx-vision"},
		"trojan": {"password":"secret"},
		"vmess": {"uuid":"11111111-1111-4111-8111-111111111111"},
		"shadowsocks": {"password":"secret"},
		"socks": {"username":"u","password":"p"},
		"http": {"username":"u","password":"p"},
		"naive": {"username":"u","password":"p"},
		"hysteria": {"auth_str":"a"},
		"hysteria2": {"password":"secret"},
		"tuic": {"uuid":"11111111-1111-4111-8111-111111111111","password":"secret"},
		"anytls": {"password":"secret"}
	}`)
	for _, typ := range SupportedInboundTypes {
		t.Run(typ, func(t *testing.T) {
			inbound := &model.Inbound{
				Type:    typ,
				Tag:     "t",
				Addrs:   malformedAddrs,
				OutJson: json.RawMessage(`{}`),
				Options: json.RawMessage(`{"listen_port":443,"method":"aes-128-gcm"}`),
			}
			// The assertion is simply that this does not panic; a malformed
			// addr may legitimately yield empty or partial links.
			_ = Generate(clientConfig, inbound, "example.com")
		})
	}
}

func TestLinkGeneratorRoundTripCommonProtocols(t *testing.T) {
	clientConfig := json.RawMessage(`{
		"vless": {"uuid":"11111111-1111-4111-8111-111111111111","flow":"xtls-rprx-vision"},
		"trojan": {"password":"trojan-secret"},
		"vmess": {"uuid":"22222222-2222-4222-8222-222222222222"},
		"shadowsocks": {"password":"ss-pass"},
		"naive": {"username":"naive-user","password":"naive-pass"},
		"hysteria2": {"password":"hy2-pass"},
		"anytls": {"password":"anytls-pass"}
	}`)

	tests := []struct {
		name     string
		inbound  *model.Inbound
		expected map[string]interface{}
	}{
		{
			name: "vless grpc tls",
			inbound: roundTripInbound(
				"vless",
				"vless-in",
				`{"listen_port":443,"transport":{"type":"grpc","service_name":"grpc-svc"}}`,
				`{}`,
				roundTripAddrs("vless.example.com", 443, "-grpc"),
			),
			expected: map[string]interface{}{
				"type":                   "vless",
				"tag":                    "vless-in-grpc",
				"server":                 "vless.example.com",
				"server_port":            443,
				"uuid":                   "11111111-1111-4111-8111-111111111111",
				"tls.enabled":            true,
				"tls.server_name":        "edge.example.com",
				"tls.utls.fingerprint":   "chrome",
				"transport.type":         "grpc",
				"transport.service_name": "grpc-svc",
			},
		},
		{
			name: "trojan ws tls",
			inbound: roundTripInbound(
				"trojan",
				"trojan-in",
				`{"listen_port":443,"transport":{"type":"ws","path":"/ws","headers":{"Host":"ws.example.com"}}}`,
				`{}`,
				roundTripAddrs("trojan.example.com", 443, "-ws"),
			),
			expected: map[string]interface{}{
				"type":                   "trojan",
				"tag":                    "trojan-in-ws",
				"server":                 "trojan.example.com",
				"server_port":            443,
				"password":               "trojan-secret",
				"tls.enabled":            true,
				"transport.type":         "ws",
				"transport.path":         "/ws",
				"transport.headers.Host": "ws.example.com",
			},
		},
		{
			name: "vmess ws tls",
			inbound: roundTripInbound(
				"vmess",
				"vmess-in",
				`{"listen_port":443,"transport":{"type":"ws","path":"/vm","headers":{"Host":"vm.example.com"}}}`,
				`{}`,
				roundTripAddrs("vmess.example.com", 443, "-ws"),
			),
			expected: map[string]interface{}{
				"type":                   "vmess",
				"tag":                    "vmess-in-ws",
				"server":                 "vmess.example.com",
				"server_port":            "443",
				"uuid":                   "22222222-2222-4222-8222-222222222222",
				"tls.enabled":            true,
				"transport.type":         "ws",
				"transport.path":         "/vm",
				"transport.headers.Host": "vm.example.com",
			},
		},
		{
			name: "shadowsocks",
			inbound: roundTripInbound(
				"shadowsocks",
				"ss-in",
				`{"listen_port":8388,"method":"aes-128-gcm"}`,
				`{}`,
				`[{"server":"ss.example.com","server_port":8388,"remark":"-node"}]`,
			),
			expected: map[string]interface{}{
				"type":        "shadowsocks",
				"tag":         "ss-in-node",
				"server":      "ss.example.com",
				"server_port": 8388,
				"method":      "aes-128-gcm",
				"password":    "ss-pass",
			},
		},
		{
			name: "hysteria2 salamander mport",
			inbound: roundTripInbound(
				"hysteria2",
				"hy2-in",
				`{"listen_port":443,"up_mbps":100,"down_mbps":200,"tcp_fast_open":true,"obfs":{"type":"salamander","password":"obfs-pass"}}`,
				`{"server_ports":["8443:8444","9443"]}`,
				roundTripAddrs("hy2.example.com", 443, "-mport"),
			),
			expected: map[string]interface{}{
				"type":          "hysteria2",
				"tag":           "hy2-in-mport",
				"server":        "hy2.example.com",
				"server_port":   443,
				"password":      "hy2-pass",
				"up_mbps":       200,
				"down_mbps":     100,
				"fastopen":      true,
				"server_ports":  []string{"8443:8444", "9443"},
				"obfs.type":     "salamander",
				"obfs.password": "obfs-pass",
				"tls.enabled":   true,
			},
		},
		{
			name: "anytls tls",
			inbound: roundTripInbound(
				"anytls",
				"anytls-in",
				`{"listen_port":443}`,
				`{}`,
				roundTripAddrs("anytls.example.com", 443, "-node"),
			),
			expected: map[string]interface{}{
				"type":            "anytls",
				"tag":             "anytls-in-node",
				"server":          "anytls.example.com",
				"server_port":     443,
				"password":        "anytls-pass",
				"tls.enabled":     true,
				"tls.server_name": "edge.example.com",
			},
		},
		{
			name: "naive http2",
			inbound: roundTripInbound(
				"naive",
				"naive-in",
				`{"listen_port":443,"tcp_fast_open":true}`,
				`{}`,
				roundTripAddrs("naive.example.com", 443, "-node"),
			),
			expected: map[string]interface{}{
				"type":            "naive",
				"tag":             "naive-in-node",
				"server":          "naive.example.com",
				"server_port":     443,
				"username":        "naive-user",
				"password":        "naive-pass",
				"tls.enabled":     true,
				"tls.server_name": "edge.example.com",
				"tls.alpn":        []string{"h2", "http/1.1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			links := Generate(clientConfig, tt.inbound, "fallback.example.com")
			if len(links) != 1 {
				t.Fatalf("expected one generated link, got %d: %#v", len(links), links)
			}

			outbound, tag, err := Parse(links[0], 0)
			if err != nil {
				t.Fatalf("round-trip parse failed for %s: %v", links[0], err)
			}
			if want, _ := tt.expected["tag"].(string); tag != want {
				t.Fatalf("expected tag %q, got %q", want, tag)
			}
			assertOutboundFields(t, *outbound, tt.expected)
		})
	}
}

func TestLinkGeneratorDeterministicMultiAddressOrder(t *testing.T) {
	clientConfig := json.RawMessage(`{
		"trojan": {"password":"trojan-secret"}
	}`)
	inbound := roundTripInbound(
		"trojan",
		"trojan-in",
		`{"listen_port":443,"transport":{"type":"ws","path":"/ws","headers":{"Host":"front.example.com"}}}`,
		`{}`,
		`[
			{"server":"first.example.com","server_port":443,"remark":"-first","tls":{"enabled":true,"server_name":"first.example.com"}},
			{"server":"second.example.com","server_port":443,"remark":"-second","tls":{"enabled":true,"server_name":"second.example.com"}}
		]`,
	)

	first := Generate(clientConfig, inbound, "fallback.example.com")
	if len(first) != 2 {
		t.Fatalf("expected two generated links, got %d: %#v", len(first), first)
	}
	for i := 0; i < 5; i++ {
		next := Generate(clientConfig, inbound, "fallback.example.com")
		if !reflect.DeepEqual(next, first) {
			t.Fatalf("generated links changed on run %d:\nfirst=%#v\nnext=%#v", i+1, first, next)
		}
	}

	wantTags := []string{"trojan-in-first", "trojan-in-second"}
	for i, link := range first {
		_, tag, err := Parse(link, 0)
		if err != nil {
			t.Fatalf("generated link %d is not parseable: %v", i, err)
		}
		if tag != wantTags[i] {
			t.Fatalf("link %d tag = %q, want %q; links=%#v", i, tag, wantTags[i], first)
		}
	}
}

func assertOutboundFields(t *testing.T, outbound map[string]interface{}, expected map[string]interface{}) {
	t.Helper()
	for path, want := range expected {
		got := nestedOutboundValue(outbound, path)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("expected %s=%#v, got %#v in %#v", path, want, got, outbound)
		}
	}
}

func roundTripInbound(typ, tag, options, outJSON, addrs string) *model.Inbound {
	return &model.Inbound{
		Type:    typ,
		Tag:     tag,
		Addrs:   json.RawMessage(addrs),
		OutJson: json.RawMessage(outJSON),
		Options: json.RawMessage(options),
	}
}

func roundTripAddrs(server string, port int, remark string) string {
	b, _ := json.Marshal([]map[string]interface{}{
		{
			"server":      server,
			"server_port": port,
			"remark":      remark,
			"tls": map[string]interface{}{
				"enabled":     true,
				"server_name": "edge.example.com",
				"alpn":        []string{"h2", "http/1.1"},
				"utls": map[string]interface{}{
					"enabled":     true,
					"fingerprint": "chrome",
				},
			},
		},
	})
	return string(b)
}

func generateTUICLinkForTest(t *testing.T, outJSON string) string {
	t.Helper()

	clientConfig := json.RawMessage(`{
		"tuic": {
			"uuid": "11111111-1111-4111-8111-111111111111",
			"password": "secret"
		}
	}`)
	inbound := &model.Inbound{
		Type:    "tuic",
		Tag:     "tuic-test",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(outJSON),
		Options: json.RawMessage(`{
			"listen_port": 443,
			"congestion_control": "bbr"
		}`),
	}

	links := Generate(clientConfig, inbound, "example.com")
	if len(links) != 1 {
		t.Fatalf("expected one generated link, got %d: %#v", len(links), links)
	}
	return links[0]
}
