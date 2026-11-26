package singboxconfig

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
)

func TestZapretDialedDNSDetourPreflight(t *testing.T) {
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
			for _, kind := range []string{"https", "tcp", "udp"} {
				config := fmt.Appendf(nil, `{"dns":{"servers":[{"type":%q,"server":"192.0.2.1","detour":"a"}]},"outbounds":%s}`, kind, tt.targets)
				err := validateDialedDNSDetours(config, map[string]bool{"a": tt.selected}, tt.extended)
				if tt.code == "" && err != nil || tt.code != "" && (err == nil || !strings.Contains(err.Error(), tt.code)) {
					t.Fatalf("%s: want %q, got %v", kind, tt.code, err)
				}
			}
		})
	}
}

func TestZapretDNSRequirementsRespectExplicitTransport(t *testing.T) {
	for _, tt := range []struct{ name, server, code string }{
		{"unsupported", `{"type":"local","detour":"a"}`, "dns_zapret_transport_unsupported"},
		{"no QUIC build capability", `{"type":"h3","detour":"a"}`, "dns_zapret_quic_unavailable"},
		{"no bootstrap", `{"type":"https","server":"dns.example","detour":"a"}`, "dns_zapret_bootstrap_required"},
		{"invalid port", `{"type":"tls","server":"2001:db8::1","server_port":70000,"detour":"a"}`, "dns_zapret_port_invalid"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := zapretDNSRequirements(fmt.Appendf(nil, `{"dns":{"servers":[%s]}}`, tt.server), map[string]bool{"a": true}, false)
			if err == nil || !strings.Contains(err.Error(), tt.code) {
				t.Fatalf("want %q, got %v", tt.code, err)
			}
		})
	}
	config := []byte(`{"dns":{"servers":[
		{"type":"https","server":"dns.example","domain_resolver":"bootstrap","detour":"a"},
		{"type":"tls","server":"192.0.2.1","server_port":18853,"detour":"b"},
		{"type":"quic","server":"dns.example","domain_resolver":{"server":"bootstrap","strategy":"ipv4_only"},"detour":"a"},
		{"type":"h3","server":"2001:db8::1","detour":"b"},
		{"type":"tcp","server":"192.0.2.1","detour":"a"},
		{"type":"udp","server":"2001:db8::1","server_port":1053,"detour":"b"},
		{"type":"https","server":"192.0.2.2"},{"type":"https","server":"192.0.2.3","detour":"proxy"}
	]}}`)
	got, err := zapretDNSRequirements(config, map[string]bool{"a": true, "b": true}, true)
	want := map[string][]zapretactivation.DNSPathRequirement{
		"a": {{Port: 443}, {Port: 853, Family: zapretactivation.DNSFamilyIPv4, QUIC: true}, {Port: 53, Family: zapretactivation.DNSFamilyIPv4}},
		"b": {{Port: 18853, Family: zapretactivation.DNSFamilyIPv4}, {Port: 443, Family: zapretactivation.DNSFamilyIPv6, QUIC: true}, {Port: 1053, Family: zapretactivation.DNSFamilyIPv6, PlainUDP: true}},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected protected DNS path requirements: %v, %v", got, err)
	}
}

func TestZapretDNSAdmissionDoesNotChangeTLSVerification(t *testing.T) {
	for _, kind := range []string{"tls", "https", "quic", "h3"} {
		t.Run(kind, func(t *testing.T) {
			selected := map[string]bool{"a": true}
			authenticated := fmt.Appendf(nil, `{"dns":{"servers":[{"type":%q,"server":"192.0.2.3","detour":"a"}]}}`, kind)
			operatorChoice := fmt.Appendf(nil, `{"dns":{"servers":[{"type":%q,"server":"192.0.2.3","detour":"a","tls":{"insecure":true}}]}}`, kind)
			original := string(operatorChoice)
			want, err := zapretDNSRequirements(authenticated, selected, true)
			if err != nil || len(want["a"]) != 1 {
				t.Fatalf("missing packet admission requirement: %v, %v", want, err)
			}
			got, err := zapretDNSRequirements(operatorChoice, selected, true)
			if err != nil || !reflect.DeepEqual(got, want) || string(operatorChoice) != original {
				t.Fatalf("TLS option changed the packet scope or candidate: %v, %v", got, err)
			}
		})
	}
}

func TestZapretDNSRequirementDeduplicationKeepsLargeCatalogOrder(t *testing.T) {
	var servers []string
	var want []zapretactivation.DNSPathRequirement
	for port := uint16(1064); port >= 1000; port-- {
		servers = append(servers,
			fmt.Sprintf(`{"type":"tcp","server":"192.0.2.1","server_port":%d,"detour":"a"}`, port),
			fmt.Sprintf(`{"type":"tls","server":"192.0.2.2","server_port":%d,"detour":"a"}`, port))
		want = append(want, zapretactivation.DNSPathRequirement{Port: port, Family: zapretactivation.DNSFamilyIPv4})
	}
	servers = append(servers,
		`{"type":"tcp","server":"192.0.2.1","server_port":1000,"detour":"b"}`,
		`{"type":"udp","server":"192.0.2.1","server_port":1000,"detour":"a"}`)
	config := fmt.Appendf(nil, `{"dns":{"servers":[%s]}}`, strings.Join(servers, ","))
	got, err := zapretDNSRequirements(config, map[string]bool{"a": true, "b": true}, false)
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, zapretactivation.DNSPathRequirement{Port: 1000, Family: zapretactivation.DNSFamilyIPv4, PlainUDP: true})
	if !reflect.DeepEqual(got, map[string][]zapretactivation.DNSPathRequirement{
		"a": want,
		"b": {{Port: 1000, Family: zapretactivation.DNSFamilyIPv4}},
	}) {
		t.Fatalf("lost packet scope or first-occurrence order: %v", got)
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
