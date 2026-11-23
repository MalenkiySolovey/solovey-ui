package singboxconfig

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestZapretEncryptedDNSDetourPreflight(t *testing.T) {
	for _, tt := range []struct {
		name, targets, code string
		selected, extended  bool
	}{
		{"removed", `[]`, "dns_detour_missing", false, false},
		{"ambiguous", `[{"type":"direct","tag":"a"},{"type":"socks","tag":"a"}]`, "dns_detour_missing", true, false},
		{"stale desired selection", `[]`, "dns_detour_missing", true, false},
		{"official binding switched off", `[{"type":"direct","tag":"a","tcp_fast_open":false}]`, "dns_detour_empty_direct", false, false},
		{"official admitted binding", `[{"type":"direct","tag":"a"}]`, "", true, false},
		{"extended ordinary direct", `[{"type":"direct","tag":"a"}]`, "", false, true},
		{"ordinary configured direct", `[{"type":"direct","tag":"a","connect_timeout":"5s"}]`, "", false, false},
		{"ordinary proxy", `[{"type":"socks","tag":"a"}]`, "", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := fmt.Appendf(nil, `{"dns":{"servers":[{"type":"https","server":"192.0.2.1","detour":"a"}]},"outbounds":%s}`, tt.targets)
			err := validateEncryptedDNSDetours(config, map[string]bool{"a": tt.selected}, tt.extended)
			if tt.code == "" && err != nil || tt.code != "" && (err == nil || !strings.Contains(err.Error(), tt.code)) {
				t.Fatalf("want %q, got %v", tt.code, err)
			}
		})
	}
}

func TestZapretDNSPortsRespectExplicitTransportAndTLS(t *testing.T) {
	for _, tt := range []struct{ name, server, code string }{
		{"unsupported", `{"type":"h3","detour":"a"}`, "dns_zapret_transport_unsupported"},
		{"insecure", `{"type":"tls","server":"192.0.2.3","detour":"a","tls":{"insecure":true}}`, "dns_zapret_tls_required"},
		{"no bootstrap", `{"type":"https","server":"dns.example","detour":"a"}`, "dns_zapret_bootstrap_required"},
		{"ipv6", `{"type":"tls","server":"2001:db8::1","detour":"a"}`, "dns_zapret_ipv6_unsupported"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ZapretDNSPorts(fmt.Appendf(nil, `{"dns":{"servers":[%s]}}`, tt.server), map[string]bool{"a": true})
			if err == nil || !strings.Contains(err.Error(), tt.code) {
				t.Fatalf("want %q, got %v", tt.code, err)
			}
		})
	}
	config := []byte(`{"dns":{"servers":[
		{"type":"https","server":"dns.example","domain_resolver":"bootstrap","detour":"a"},
		{"type":"tls","server":"192.0.2.1","server_port":18853,"detour":"b"},
		{"type":"https","server":"192.0.2.2"},{"type":"https","server":"192.0.2.3","detour":"proxy"}
	]}}`)
	got, err := ZapretDNSPorts(config, map[string]bool{"a": true, "b": true})
	if err != nil || !reflect.DeepEqual(got, map[string][]uint16{"a": {443}, "b": {18853}}) {
		t.Fatalf("unexpected protected DNS ports: %v, %v", got, err)
	}
}

func TestDNSBootstrapCycleRejectedWithoutChangingCandidate(t *testing.T) {
	const config = `{"dns":{"servers":[
		{"type":"https","tag":"a","server":"a.example","domain_resolver":"b"},
		{"type":"tls","tag":"b","server":"b.example","domain_resolver":{"server":"a"}}
	]}}`
	projection, err := PrepareDNSUpgrade([]byte(config))
	if err == nil || !strings.Contains(err.Error(), "dns_bootstrap_cycle") || string(projection.Candidate) != config {
		t.Fatalf("bootstrap validation did not preserve the rejected candidate: %v", err)
	}
}
