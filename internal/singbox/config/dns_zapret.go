package singboxconfig

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
)

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
