package singboxconfig

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"reflect"
	"slices"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

// ValidateDialedDNSDetours checks the completed candidate without starting
// transports. The dry Outbounds projection omits broker paths; selected names
// denote only the Directs that would receive one after actual admission.
func ValidateDialedDNSDetours(config []byte, selected map[string]bool) error {
	return validateDialedDNSDetours(config, selected, coreprovider.Current().ID == "shtorm-extended")
}

func validateDialedDNSDetours(config []byte, selected map[string]bool, allowEmptyDirect bool) error {
	var root, dns dnsObject
	var servers []dnsObject
	_ = json.Unmarshal(config, &root)
	_ = json.Unmarshal(root["dns"], &dns)
	_ = json.Unmarshal(dns["servers"], &servers)
	targets := make(map[string][]dnsObject)
	for _, section := range []string{"outbounds", "endpoints"} {
		var rows []dnsObject
		_ = json.Unmarshal(root[section], &rows)
		for _, row := range rows {
			if tag := dnsString(row, "tag"); tag != "" {
				targets[tag] = append(targets[tag], row)
			}
		}
	}
	for i, server := range servers {
		kind, ref := dnsString(server, "type"), dnsString(server, "detour")
		if ref == "" || !zapretDialedDNS(kind) {
			continue
		}
		fail := func(code, message string) error {
			return diagnostics.FirstError([]diagnostics.Finding{dnsFailure(fmt.Sprintf("dns.servers[%d].detour", i), code, message, diagnostics.ManualRequired)})
		}
		rows := targets[ref]
		if len(rows) != 1 {
			return fail("dns_detour_missing", "Select one existing outbound or endpoint for this DNS transport.")
		}
		// Pinned Official rejects an empty Direct lazily, not on JSON decode.
		if !allowEmptyDirect && !selected[ref] && dnsString(rows[0], "type") == C.TypeDirect {
			var dial option.DialerOptions
			if json.Unmarshal(dnsJSON(rows[0]), &dial) == nil && reflect.DeepEqual(dial, option.DialerOptions{}) {
				return fail("dns_detour_empty_direct", "This Core cannot use an empty Direct as an explicit DNS detour. Update the DNS transport before removing its dial settings or disabling its Zapret binding.")
			}
		}
	}
	return nil
}

func zapretDialedDNS(kind string) bool {
	switch kind {
	case C.DNSTypeTCP, C.DNSTypeUDP, C.DNSTypeHTTPS, C.DNSTypeTLS, C.DNSTypeQUIC, C.DNSTypeHTTP3:
		return true
	default:
		return false
	}
}

// ZapretDNSRequirements derives requirements for existing native detours, without
// creating DNS-specific bindings or modifying TLS, bootstrap, or DNS rules.
func ZapretDNSRequirements(config []byte, selected map[string]bool) (map[string][]zapretactivation.DNSPathRequirement, error) {
	return zapretDNSRequirements(config, selected, C.WithQUIC)
}

func zapretDNSRequirements(config []byte, selected map[string]bool, withQUIC bool) (map[string][]zapretactivation.DNSPathRequirement, error) {
	servers, _ := dnsTransportSections(config)
	var requirements map[string][]zapretactivation.DNSPathRequirement
	for i, server := range servers {
		ref := dnsString(server, "detour")
		if !selected[ref] {
			continue
		}
		fail := func(field, code, message string) error {
			return diagnostics.FirstError([]diagnostics.Finding{dnsFailure(fmt.Sprintf("dns.servers[%d].%s", i, field), code, message, diagnostics.ManualRequired)})
		}
		var need zapretactivation.DNSPathRequirement
		kind := dnsString(server, "type")
		switch kind {
		case C.DNSTypeTCP, C.DNSTypeUDP:
			need.Port = 53
			need.PlainUDP = kind == C.DNSTypeUDP
		case C.DNSTypeHTTPS, C.DNSTypeHTTP3:
			need.Port = 443
		case C.DNSTypeTLS, C.DNSTypeQUIC:
			need.Port = 853
		default:
			return nil, fail("detour", "dns_zapret_transport_unsupported", "Select a remote TCP, UDP, DoH, DoT, DoQ or DoH3 transport for an enabled Zapret Direct.")
		}
		need.QUIC = kind == C.DNSTypeQUIC || kind == C.DNSTypeHTTP3
		if need.QUIC && !withQUIC {
			return nil, fail("detour", "dns_zapret_quic_unavailable", "This Core build does not support DoQ or DoH3.")
		}
		if raw, ok := server["server_port"]; ok {
			var override uint16
			if json.Unmarshal(raw, &override) != nil {
				return nil, fail("server_port", "dns_zapret_port_invalid", "Select a valid port for the protected DNS server.")
			}
			if override != 0 {
				need.Port = override
			}
		}
		if ip, err := netip.ParseAddr(dnsString(server, "server")); err == nil {
			need.Family = zapretactivation.DNSFamilyIPv4
			if ip.Unmap().Is6() {
				need.Family = zapretactivation.DNSFamilyIPv6
			}
		} else {
			var resolver option.DomainResolveOptions
			if json.Unmarshal(server["domain_resolver"], &resolver) != nil || resolver.Server == "" {
				return nil, fail("domain_resolver", "dns_zapret_bootstrap_required", "Select an explicit nonrecursive bootstrap resolver for the DNS endpoint hostname.")
			}
			switch C.DomainStrategy(resolver.Strategy) {
			case C.DomainStrategyIPv4Only:
				need.Family = zapretactivation.DNSFamilyIPv4
			case C.DomainStrategyIPv6Only:
				need.Family = zapretactivation.DNSFamilyIPv6
			}
		}
		// Resolver authentication is a Core TLS option, not a packet admission
		// requirement. Keep the operator's setting and the same egress scope.
		if requirements == nil {
			requirements = make(map[string][]zapretactivation.DNSPathRequirement)
		}
		if !slices.Contains(requirements[ref], need) {
			requirements[ref] = append(requirements[ref], need)
		}
	}
	return requirements, nil
}
