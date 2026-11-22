package singboxconfig

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"reflect"
	"slices"

	coreprovider "github.com/MalenkiySolovey/solovey-ui/core/provider"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/sagernet/sing-box/option"
)

// ValidateEncryptedDNSDetours checks the completed candidate without starting
// transports. The dry Outbounds projection omits broker paths; selected names
// denote only the Directs that would receive one after actual admission.
func ValidateEncryptedDNSDetours(config []byte, selected map[string]bool) error {
	return validateEncryptedDNSDetours(config, selected, coreprovider.Current().ID == "shtorm-extended")
}

func validateEncryptedDNSDetours(config []byte, selected map[string]bool, allowEmptyDirect bool) error {
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
		if ref == "" || kind != "https" && kind != "tls" {
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
		if !allowEmptyDirect && !selected[ref] && dnsString(rows[0], "type") == "direct" {
			var dial option.DialerOptions
			if json.Unmarshal(dnsJSON(rows[0]), &dial) == nil && reflect.DeepEqual(dial, option.DialerOptions{}) {
				return fail("dns_detour_empty_direct", "This Core cannot use an empty Direct as an explicit DNS detour. Update the DNS transport before removing its dial settings or disabling its Zapret binding.")
			}
		}
	}
	return nil
}

// ZapretDNSPorts derives requirements for existing native detours, without
// creating DNS-specific bindings or modifying TLS, bootstrap, or DNS rules.
func ZapretDNSPorts(config []byte, selected map[string]bool) (map[string][]uint16, error) {
	servers, _ := dnsTransportSections(config)
	var ports map[string][]uint16
	for i, server := range servers {
		ref := dnsString(server, "detour")
		if !selected[ref] {
			continue
		}
		fail := func(field, code, message string) error {
			return diagnostics.FirstError([]diagnostics.Finding{dnsFailure(fmt.Sprintf("dns.servers[%d].%s", i, field), code, message, diagnostics.ManualRequired)})
		}
		var port uint16
		switch dnsString(server, "type") {
		case "https":
			port = 443
		case "tls":
			port = 853
		default:
			return nil, fail("detour", "dns_zapret_transport_unsupported", "Only DoH over HTTPS/TCP and DoT are supported through an enabled Zapret Direct.")
		}
		if raw, ok := server["server_port"]; ok {
			var override uint16
			if json.Unmarshal(raw, &override) != nil {
				return nil, fail("server_port", "dns_zapret_port_invalid", "Select a valid TCP port for the protected DNS server.")
			}
			if override != 0 {
				port = override
			}
		}
		if ip, err := netip.ParseAddr(dnsString(server, "server")); err == nil {
			if !ip.Is4() {
				return nil, fail("server", "dns_zapret_ipv6_unsupported", "This installed Zapret profile supports IPv4 DNS endpoints only.")
			}
		} else if dnsResolverTag(server["domain_resolver"]) == "" {
			return nil, fail("domain_resolver", "dns_zapret_bootstrap_required", "Select an explicit nonrecursive bootstrap resolver for the DNS endpoint hostname.")
		}
		var tls dnsObject
		_ = json.Unmarshal(server["tls"], &tls)
		if dnsBool(tls, "insecure") {
			return nil, fail("tls.insecure", "dns_zapret_tls_required", "Certificate verification must remain enabled for protected DNS.")
		}
		if ports == nil {
			ports = make(map[string][]uint16)
		}
		if !slices.Contains(ports[ref], port) {
			ports[ref] = append(ports[ref], port)
		}
	}
	return ports, nil
}
