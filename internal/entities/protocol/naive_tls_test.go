package protocol

import (
	"encoding/json"
	"testing"
)

func TestCurrentNaiveTLSRejectsUnsupportedNativePolicy(t *testing.T) {
	for _, test := range []struct{ field, value, code string }{
		{"insecure", "true", "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"disable_sni", "true", "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"alpn", `["h2"]`, "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"min_version", `"1.3"`, "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"max_version", `"1.3"`, "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"cipher_suites", `["TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"]`, "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"curve_preferences", `["X25519"]`, "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"client_certificate", `["fixture-public-certificate"]`, "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"client_certificate_path", `"/fixture/client.pem"`, "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"client_key", `["fixture-not-a-key"]`, "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"client_key_path", `"/fixture/client-key.pem"`, "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"fragment", "true", "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"record_fragment", "true", "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"fragment_fallback_delay", `"1s"`, "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"kernel_tx", "true", "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"kernel_rx", "true", "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"handshake_timeout", `"1s"`, "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"utls", `{"enabled":true,"fingerprint":"chrome"}`, "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"reality", `{"enabled":true}`, "NAIVE_TLS_OPTION_UNSUPPORTED"},
		{"certificate_public_key_sha256", `["AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="]`, "NAIVE_PUBLIC_KEY_PIN_UNSUPPORTED"},
	} {
		t.Run(test.field, func(t *testing.T) {
			source := json.RawMessage(`{"tls":{"enabled":true,"` + test.field + `":` + test.value + `}}`)
			findings := CurrentFindings("naive", "outbound", "outbound.options", source)
			found := false
			for _, finding := range findings {
				if finding.Code == test.code && finding.Path == "outbound.options.tls."+test.field {
					found = true
				}
			}
			if !found {
				t.Fatalf("unsupported native TLS policy has no owned finding: %#v", findings)
			}
		})
	}
}

func TestCurrentNaiveTLSPreservesSupportedTrustAndPartialShape(t *testing.T) {
	for _, source := range []string{
		`{"stream_receive_window":"1MB"}`,
		`{"tls":{"enabled":true,"server_name":"fixture.invalid","certificate":["fixture-public-CA"],"insecure":false,"alpn":[],"min_version":"","max_version":"","handshake_timeout":"0s","fragment":false,"kernel_rx":false,"utls":{"enabled":false}}}`,
		`{"quic":true,"quic_congestion_control":"cubic","tls":{"enabled":true,"certificate_path":"/fixture/ca.pem","ech":{"enabled":true,"query_server_name":"fixture.invalid"}}}`,
	} {
		if findings := CurrentFindings("naive", "outbound", "outbound.options", json.RawMessage(source)); len(findings) > 0 {
			t.Fatalf("supported or partial shape rejected: %#v", findings)
		}
	}
	for _, kind := range []string{"naive", "hysteria", "hysteria2"} {
		if findings := CurrentFindings(kind, "inbound", "inbound.options", json.RawMessage(`{"tls":{"enabled":true,"alpn":["h2"],"min_version":"1.2","max_version":"1.3"}}`)); len(findings) > 0 {
			t.Fatalf("Naive outbound restriction reached another consumer: %#v", findings)
		}
	}
}

func TestStoredNaiveTLSRequiresEnabledTrustPolicy(t *testing.T) {
	for _, source := range []string{`{"server":"127.0.0.1"}`, `{"tls":null}`, `{"tls":{"enabled":false}}`} {
		findings := StoredOutboundFindings("naive", "outbound.options", json.RawMessage(source))
		if len(findings) == 0 || findings[0].Code != "NAIVE_TLS_REQUIRED" {
			t.Fatalf("stored Naive lacks required TLS refusal: %#v", findings)
		}
		if findings := ConfigFindings([]byte(`{"outbounds":[{"type":"naive",` + source[1:] + `]}`)); len(findings) == 0 || findings[0].Code != "NAIVE_TLS_REQUIRED" {
			t.Fatalf("complete config bypassed the same TLS owner: %#v", findings)
		}
	}
	for _, test := range []struct{ source, code string }{
		{`{"tls":{"enabled":true,"engine":"windows"}}`, "TLS_ENGINE_UNAVAILABLE"},
		{`{"tls":{"enabled":true,"spoof":"fixture"}}`, "TLS_SPOOF_UNAVAILABLE"},
	} {
		findings := StoredOutboundFindings("naive", "outbound.options", json.RawMessage(test.source))
		if len(findings) == 0 || findings[0].Code != test.code {
			t.Fatal("Naive bypassed shared TLS authorization")
		}
	}
}
