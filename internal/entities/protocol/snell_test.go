package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSnellPinnedVersionValidation(t *testing.T) {
	for _, tc := range []struct{ side, data, code string }{
		{"inbound", `{"version":5,"psk":"fixture","obfs_mode":"http","users":[{"name":"a","userkey":"key-a"},{"name":"b","userkey":"key-b"}]}`, ""},
		{"inbound", `{"version":6,"psk":"fixture-psk-12","mode":"unshaped"}`, ""},
		{"outbound", `{"version":4,"psk":"fixture","obfs_mode":"http","obfs_host":"example.invalid","userkey":"key-a"}`, ""},
		{"outbound", `{"version":6,"psk":"fixture-psk-12","mode":"unsafe-raw","userkey":"key-a"}`, ""},
		{"inbound", `{"version":4,"psk":"fixture"}`, "SNELL_OPTIONS_INVALID"},
		{"outbound", `{"version":5,"psk":"fixture"}`, "SNELL_OPTIONS_INVALID"},
		{"inbound", `{"version":5,"psk":"fixture","mode":"default"}`, "SNELL_OPTIONS_INVALID"},
		{"inbound", `{"version":6,"psk":"fixture-psk-12","obfs_mode":"http"}`, "SNELL_OPTIONS_INVALID"},
		{"inbound", `{"version":6,"psk":"short"}`, "SNELL_PSK_LENGTH_INVALID"},
		{"inbound", `{"version":6,"psk":"абвгдеж"}`, ""},
		{"inbound", `{"version":6,"psk":"` + strings.Repeat("x", 256) + `"}`, "SNELL_PSK_LENGTH_INVALID"},
		{"inbound", `{"version":6,"psk":"fixture-psk-12","mode":"invalid"}`, "SNELL_MODE_INVALID"},
		{"inbound", `{"version":5,"psk":"fixture","obfs_mode":"invalid"}`, "SNELL_OBFS_INVALID"},
		{"inbound", `{"version":5,"psk":"fixture","users":[{"name":"a","userkey":"same"},{"name":"b","userkey":"same"}]}`, "SNELL_USERS_INVALID"},
		{"inbound", `{"version":5,"psk":"fixture","users":[{"name":"a","userkey":""}]}`, "SNELL_USERS_INVALID"},
	} {
		findings := CurrentFindings("snell", tc.side, "entity", json.RawMessage(tc.data))
		if tc.code == "" {
			if len(findings) != 0 {
				t.Fatalf("valid %s: %v", tc.side, findings)
			}
			continue
		}
		if len(findings) != 1 || findings[0].Code != tc.code {
			t.Fatalf("expected %s, got %v", tc.code, findings)
		}
		if strings.Contains(findings[0].Message, "fixture") {
			t.Fatal("credential reflected in diagnosis")
		}
	}
}
