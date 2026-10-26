//go:build shtorm_extended && with_quic

package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeInboundOptionAdmissionUsesPinnedSchema(t *testing.T) {
	for _, fixture := range []struct {
		kind, raw, code string
	}{
		{"ssh", `{"server_version":"SSH-2.0-Solovey","max_auth_tries":3,"host_key_path":["/fixture/key-a","/fixture/key-b"]}`, ""},
		{"ssh", `{"host_key":{"intent":"replace","value":"private-invalid"}}`, ""},
		{"ssh", `{"server_version":"SSH-2.0-private-invalid\n"}`, "SSH_SERVER_VERSION_INVALID"},
		{"ssh", `{"server_version":"SSH-2.0-"}`, "SSH_SERVER_VERSION_INVALID"},
		{"ssh", `{"server_version":"SSH-2.0-private-invalid\u0000"}`, "SSH_SERVER_VERSION_INVALID"},
		{"ssh", `{"max_auth_tries":"private-invalid"}`, "NATIVE_OPTIONS_INVALID"},
		{"ssh", `{"unknown_native_field":"private-invalid"}`, "NATIVE_OPTIONS_INVALID"},
		{"mtproxy", `{"concurrency":4,"domain_fronting_host":"fixture.example","domain_fronting_port":443,"domain_fronting_proxy_protocol":true,"prefer_ip":"only-ipv4","auto_update":false,"allow_fallback_on_unknown_dc":true,"idle_timeout":"1m","handshake_timeout":"8s","tolerate_time_skewness":"30s","throttle_check_interval":"10s","throttle_max_connections":20,"doppelganger_each":"2h","doppelganger_urls":["https://fixture.example/"],"doppelganger_per_raid":2,"doppelganger_drs":true}`, ""},
		{"mtproxy", `{"concurrency":-1}`, "NATIVE_OPTIONS_INVALID"},
		{"mtproxy", `{"idle_timeout":"private-invalid"}`, "NATIVE_OPTIONS_INVALID"},
		{"mtproxy", `{"unknown_native_field":"private-invalid"}`, "NATIVE_OPTIONS_INVALID"},
		{"mieru", `{"transport":"TCP","listen_ports":"23456-23457","user_hint_is_mandatory":true,"mtu":1400}`, ""},
		{"mieru", `{"transport":"UDP","listen_port":23456}`, ""},
		{"mieru", `{"transport":{"type":"tcp"}}`, "NATIVE_OPTIONS_INVALID"},
		{"mieru", `{"transport":"private-invalid"}`, "MIERU_TRANSPORT_INVALID"},
		{"mieru", `{"transport":"TCP","unknown_native_field":"private-invalid"}`, "NATIVE_OPTIONS_INVALID"},
		{"mieru", `{"transport":"TCP","mtu":-1}`, "NATIVE_OPTIONS_INVALID"},
		{"trusttunnel", `{"network":["tcp","udp"],"congestion_controller":"bbr","cwnd":48,"disable_path_mtu_discovery":true}`, ""},
		{"trusttunnel", `{"network":"tcp","congestion_controller":"cubic"}`, ""},
		{"trusttunnel", `{"network":"udp","congestion_controller":"reno"}`, ""},
		{"trusttunnel", `{"network":["tcp","private-invalid"]}`, "NATIVE_OPTIONS_INVALID"},
		{"trusttunnel", `{"congestion_controller":"bbr_standard"}`, "TRUSTTUNNEL_CONGESTION_UNSUPPORTED"},
		{"trusttunnel", `{"congestion_controller":"bbr2"}`, "TRUSTTUNNEL_CONGESTION_UNSUPPORTED"},
		{"trusttunnel", `{"congestion_controller":"bbr2_variant"}`, "TRUSTTUNNEL_CONGESTION_UNSUPPORTED"},
		{"trusttunnel", `{"unknown_native_field":"private-invalid"}`, "NATIVE_OPTIONS_INVALID"},
	} {
		findings := CurrentFindings(fixture.kind, "inbound", "inbound.options", json.RawMessage(fixture.raw))
		if fixture.code == "" {
			if len(findings) != 0 {
				t.Fatalf("pinned schema rejected %s options: %s", fixture.kind, findings[0].Code)
			}
		} else if len(findings) != 1 || findings[0].Code != fixture.code {
			t.Fatalf("%s schema admission returned %#v, expected %s", fixture.kind, findings, fixture.code)
		}
		encoded, _ := json.Marshal(findings)
		if strings.Contains(string(encoded), "private-invalid") {
			t.Fatal("native option diagnostic disclosed an input value")
		}
	}
}
