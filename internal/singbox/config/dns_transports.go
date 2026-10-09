package singboxconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/sagernet/sing-box/option"
)

func dnsTransportSections(config []byte) (servers, services []dnsObject) {
	var root, dns dnsObject
	if json.Unmarshal(config, &root) != nil {
		return
	}
	_ = json.Unmarshal(root["dns"], &dns)
	_ = json.Unmarshal(dns["servers"], &servers)
	_ = json.Unmarshal(root["services"], &services)
	return
}

// DNSSelectedTransportFindings validates portable configuration identities.
// Host observations belong to DNSRuntimeFindings, not subscription templates.
func DNSSelectedTransportFindings(config []byte) []diagnostics.Finding {
	servers, services := dnsTransportSections(config)
	var root dnsObject
	_ = json.Unmarshal(config, &root)
	_, completeServices := root["services"]
	var findings []diagnostics.Finding
	fail := func(path, code, message string) {
		findings = append(findings, dnsFailure(path, code, message, diagnostics.ManualRequired))
	}
	resolvedServices := 0
	for i, service := range services {
		if dnsString(service, "type") == "resolved" {
			resolvedServices++
			if resolvedServices > 1 {
				fail(fmt.Sprintf("services[%d]", i), "dns_resolved_service_duplicate", "The official resolve1 service has one owner per core; retain one resolved service.")
			}
		}
	}
	resolvedTransports := 0
	ctx := registry.Context(context.Background())
	for i, server := range servers {
		kind := dnsString(server, "type")
		if kind != "mdns" && kind != "resolved" {
			continue
		}
		path := fmt.Sprintf("dns.servers[%d]", i)
		var decoded option.DNSServerOptions
		if decoded.UnmarshalJSONContext(ctx, dnsJSON(server)) != nil {
			fail(path, "dns_transport_schema_rejected", "The pinned DNS transport schema rejected this configuration. Correct its field types and unknown fields.")
			continue
		}
		if kind == "mdns" {
			options := decoded.Options.(*option.MDNSDNSServerOptions)
			if !validMDNSInterfaces(options.Interface) {
				fail(path+".interface", "dns_mdns_interface_invalid", "Specify at most 64 distinct nonempty interface names without surrounding whitespace or control characters; omit the list to use eligible interfaces.")
			}
			if options.PreferGo || len(options.NeighborDomain) > 0 {
				fail(path, "dns_mdns_local_option_ignored", "mDNS uses fixed local and link-local reverse domains. Remove prefer_go and neighbor_domain, which this transport does not apply.")
			}
			continue
		}
		resolvedTransports++
		if resolvedTransports > 1 {
			fail(path, "dns_resolved_transport_duplicate", "The official resolved service has one DNS callback owner; retain one resolved DNS transport.")
		}
		reference := decoded.Options.(*option.ResolvedDNSServerOptions).Service
		if reference == "" {
			fail(path+".service", "dns_resolved_service_required", "Select the existing resolved service owned by this core.")
			continue
		}
		if !completeServices {
			// Base configuration is completed with DB-owned services before save
			// or runtime validation. A base cannot invent their identities.
			continue
		}
		matches := 0
		valid := false
		for _, service := range services {
			if dnsString(service, "tag") == reference {
				matches++
				valid = dnsString(service, "type") == "resolved" && string(service["enabled"]) != "false"
			}
		}
		if matches != 1 || !valid {
			fail(path+".service", "dns_resolved_service_missing", "The reference must identify one available resolved service in the same core; correct missing, disabled, ambiguous or differently typed services.")
		}
	}
	return findings
}

func validMDNSInterfaces(names []string) bool {
	if len(names) > 64 {
		return false
	}
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || len(name) > 256 || strings.TrimSpace(name) != name || strings.ContainsFunc(name, unicode.IsControl) || seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}

// DNSRuntimeFindings checks the selected server runtime against an injected
// observation. It neither opens multicast sockets nor claims a D-Bus name.
func DNSRuntimeFindings(config []byte, environment entitycapabilities.Environment) []diagnostics.Finding {
	servers, services := dnsTransportSections(config)
	var findings []diagnostics.Finding
	check := func(category, kind, path string) {
		fact := entitycapabilities.ResolveWithEnvironment(category, kind, environment)
		if !fact.Available {
			findings = append(findings, dnsFailure(path, fact.Reason, "This DNS capability is unavailable for the current build or observed environment. Inspect capability diagnostics and choose an available configuration.", diagnostics.ManualRequired))
		}
	}
	for i, server := range servers {
		kind := dnsString(server, "type")
		path := fmt.Sprintf("dns.servers[%d]", i)
		if kind == "mdns" || kind == "resolved" {
			check("dns", kind, path)
		}
		if kind == "mdns" {
			var decoded option.DNSServerOptions
			// Portable validation already diagnoses malformed interface fields.
			if decoded.UnmarshalJSONContext(registry.Context(context.Background()), dnsJSON(server)) == nil {
				for _, name := range decoded.Options.(*option.MDNSDNSServerOptions).Interface {
					if !slices.Contains(environment.MulticastNames(), name) {
						findings = append(findings, dnsFailure(path+".interface", "dns_mdns_interface_unavailable", "Every explicitly selected interface must be observed up, multicast capable, non-loopback and have a usable address.", diagnostics.ManualRequired))
						break
					}
				}
			}
		}
	}
	for i, service := range services {
		if dnsString(service, "type") == "resolved" {
			check("services", "resolved", fmt.Sprintf("services[%d]", i))
		}
	}
	return findings
}

func CurrentDNSRuntimeFindings(config []byte) []diagnostics.Finding {
	if HasSelectedDNSTransport(config) {
		return DNSRuntimeFindings(config, entitycapabilities.ObserveEnvironment())
	}
	return nil
}

func HasSelectedDNSTransport(config []byte) bool {
	servers, services := dnsTransportSections(config)
	for _, section := range [][]dnsObject{servers, services} {
		for _, row := range section {
			if kind := dnsString(row, "type"); kind == "mdns" || kind == "resolved" {
				return true
			}
		}
	}
	return false
}
