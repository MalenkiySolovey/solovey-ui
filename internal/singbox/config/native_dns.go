package singboxconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/sagernet/sing-box/option"
)

// The common DNS owner validates references before saving. Native constructors
// resolve dependencies at start; a parse-only check cannot prove this graph.
func nativeDNSFindings(servers []dnsObject) []diagnostics.Finding {
	tags := make(map[string]int)
	for i, server := range servers {
		tags[dnsRuntimeTag(server, i)]++
	}
	dependencies := make(map[string][]string)
	fallbacks := make(map[string]string)
	var findings []diagnostics.Finding
	for i, server := range servers {
		kind, tag := dnsString(server, "type"), dnsRuntimeTag(server, i)
		var resolver string
		if json.Unmarshal(server["domain_resolver"], &resolver) != nil {
			var options dnsObject
			_ = json.Unmarshal(server["domain_resolver"], &options)
			resolver = dnsString(options, "server")
		}
		if resolver != "" {
			dependencies[tag] = append(dependencies[tag], resolver)
		}
		if kind != "sdns" && kind != "fallback" && kind != "openconnect" {
			continue
		}
		path := fmt.Sprintf("dns.servers[%d]", i)
		if !registry.Resolve("dns", kind).Available() {
			findings = append(findings, dnsWarning(path, "dns_native_transport_unavailable", "This DNS transport is preserved but unavailable in the selected provider build. It cannot be created, edited or used by this runtime."))
			continue
		}
		var decoded option.DNSServerOptions
		if decoded.UnmarshalJSONContext(registry.Context(context.Background()), dnsJSON(server)) != nil {
			findings = append(findings, dnsFailure(path, "dns_transport_schema_rejected", "The selected DNS transport rejected the field types or unknown options.", diagnostics.ManualRequired))
			continue
		}
		if kind != "fallback" {
			continue
		}
		fallbacks[tag] = path
		var references []string
		_ = json.Unmarshal(server["servers"], &references)
		seen := make(map[string]bool)
		if len(references) == 0 {
			findings = append(findings, dnsFailure(path+".servers", "dns_fallback_servers_required", "Select at least one existing DNS server.", diagnostics.ManualRequired))
		}
		for _, reference := range references {
			if reference == "" || reference == tag || tags[reference] != 1 || seen[reference] {
				findings = append(findings, dnsFailure(path+".servers", "dns_fallback_reference_invalid", "Fallback references must identify distinct existing DNS servers and cannot reference this server.", diagnostics.ManualRequired))
			}
			seen[reference] = true
			dependencies[tag] = append(dependencies[tag], reference)
		}
	}
	// Remove resolved nodes iteratively, including bootstrap resolver edges.
	// Remaining fallback nodes would never start in the native dependency owner.
	remaining := make(map[string]bool)
	for tag := range tags {
		remaining[tag] = true
	}
	for {
		progress := false
		for tag := range remaining {
			ready := true
			for _, dependency := range dependencies[tag] {
				if remaining[dependency] {
					ready = false
					break
				}
			}
			if ready {
				delete(remaining, tag)
				progress = true
			}
		}
		if !progress {
			break
		}
	}
	for tag, path := range fallbacks {
		if remaining[tag] {
			findings = append(findings, dnsFailure(path+".servers", "dns_fallback_dependency_cycle", "Fallback and bootstrap DNS references must form an acyclic dependency graph.", diagnostics.ManualRequired))
		}
	}
	return findings
}

// ValidateDNSCapabilityEdits permits unchanged historical objects and explicit
// removal. It cannot turn preservation into creation, mutation or conversion.
func ValidateDNSCapabilityEdits(previous, candidate []byte) error {
	oldServers, _ := dnsTransportSections(previous)
	newServers, _ := dnsTransportSections(candidate)
	oldCounts := make(map[string]int)
	oldTags := make(map[string]string)
	for _, server := range oldServers {
		fact := registry.Resolve("dns", dnsString(server, "type"))
		if fact.Known && !fact.Available() {
			identity := dnsSemanticIdentity(server)
			oldCounts[identity]++
			if tag := dnsString(server, "tag"); tag != "" {
				oldTags[tag] = identity
			}
		}
	}
	for i, server := range newServers {
		identity := dnsSemanticIdentity(server)
		fact := registry.Resolve("dns", dnsString(server, "type"))
		unchanged := oldCounts[identity] > 0
		if original, historical := oldTags[dnsString(server, "tag")]; historical && original != identity || fact.Known && !fact.Available() && !unchanged {
			return diagnostics.FirstError([]diagnostics.Finding{dnsFailure(fmt.Sprintf("dns.servers[%d]", i), "dns_unavailable_edit_rejected", "An unavailable DNS resource may be inspected, preserved unchanged or explicitly removed. Select a supporting provider before editing or converting it.", diagnostics.ManualRequired)})
		}
		if unchanged {
			oldCounts[identity]--
		}
	}
	return nil
}

func dnsSemanticIdentity(server dnsObject) string {
	decoder := json.NewDecoder(bytes.NewReader(dnsJSON(server)))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return ""
	}
	result, _ := json.Marshal(value)
	return string(result)
}

// ProjectDNSCapabilities changes only the effective copy. A referenced inactive
// resolver fails closed; in particular, removing the implicit first server must
// never silently select a different DNS policy.
func ProjectDNSCapabilities(config []byte) ([]byte, []diagnostics.Finding, error) {
	servers, _ := dnsTransportSections(config)
	omitted := make(map[string]bool)
	for i, server := range servers {
		fact := registry.Resolve("dns", dnsString(server, "type"))
		if fact.Known && !fact.Available() {
			omitted[dnsRuntimeTag(server, i)] = true
		}
	}
	for {
		changed := false
		for i, server := range servers {
			tag := dnsRuntimeTag(server, i)
			if omitted[tag] || dnsString(server, "type") != "fallback" {
				continue
			}
			for _, dependency := range dnsStringList(server["servers"]) {
				if omitted[dependency] {
					omitted[tag] = true
					changed = true
					break
				}
			}
		}
		if !changed {
			break
		}
	}
	if len(omitted) == 0 {
		return config, nil, nil
	}
	var root, section dnsObject
	_ = json.Unmarshal(config, &root)
	_ = json.Unmarshal(root["dns"], &section)
	final := dnsString(section, "final")
	if final == "" && len(servers) > 0 {
		final = dnsRuntimeTag(servers[0], 0)
	}
	if omitted[final] {
		finding := dnsFailure("dns.final", "dns_final_unavailable", "The selected or implicit default DNS server is unavailable. Explicitly select an available default while preserving the inactive resource.", diagnostics.ManualRequired)
		return config, []diagnostics.Finding{finding}, diagnostics.FirstError([]diagnostics.Finding{finding})
	}
	retained := make([]dnsObject, 0, len(servers))
	var findings []diagnostics.Finding
	for i, server := range servers {
		if omitted[dnsRuntimeTag(server, i)] {
			findings = append(findings, dnsWarning(fmt.Sprintf("dns.servers[%d]", i), "dns_resource_inactive", "The unavailable DNS resource is preserved in desired state and omitted from this runtime."))
		} else {
			if dnsString(server, "tag") == "" {
				server["tag"] = dnsJSON(dnsRuntimeTag(server, i))
			}
			retained = append(retained, server)
		}
	}
	section["servers"] = dnsJSON(retained)
	root["dns"] = dnsJSON(section)
	findings = append(findings, DNSReferenceFindings(dnsJSON(root))...)
	findings = append(findings, analyzeDefaultResolver(root, retained)...)
	var rules []dnsObject
	_ = json.Unmarshal(section["rules"], &rules)
	for len(rules) > 0 {
		rule := rules[0]
		rules = rules[1:]
		for _, reference := range append(dnsStringList(rule["preferred_by"]), dnsString(rule, "server")) {
			if omitted[reference] {
				findings = append(findings, dnsFailure("dns.rules", "dns_reference_unavailable", "A DNS rule references an inactive resolver. Explicitly repair that reference before applying this provider configuration.", diagnostics.ManualRequired))
			}
		}
		var children []dnsObject
		_ = json.Unmarshal(rule["rules"], &children)
		rules = append(rules, children...)
	}
	if err := diagnostics.FirstError(findings); err != nil {
		return config, findings, err
	}
	return dnsJSON(root), findings, nil
}

func dnsRuntimeTag(server dnsObject, index int) string {
	if tag := dnsString(server, "tag"); tag != "" {
		return tag
	}
	return strconv.Itoa(index)
}
