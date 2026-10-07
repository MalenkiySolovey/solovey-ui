package singboxconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/rulepolicy"
)

// DNSCompatibility is a pure owner-local projection. On any unresolved case
// Candidate is the complete original, including unrelated sections. The caller
// must validate the complete generated candidate before durable publication.
type DNSCompatibility struct {
	Original  []byte                `json:"-"`
	Candidate []byte                `json:"-"`
	Outcome   string                `json:"outcome"`
	Findings  []diagnostics.Finding `json:"findings"`
}

type dnsObject = map[string]json.RawMessage

func PrepareDNSUpgrade(config []byte) (DNSCompatibility, error) {
	return prepareDNS(config, true)
}

func prepareDNS(config []byte, historical bool) (DNSCompatibility, error) {
	result := DNSCompatibility{Original: bytes.Clone(config), Candidate: bytes.Clone(config), Outcome: diagnostics.LosslessAutomatic}
	var root dnsObject
	if json.Unmarshal(config, &root) != nil || root == nil {
		result.Findings = append(result.Findings, dnsFailure("config", "invalid_json", "Configuration must be a JSON object.", diagnostics.UnsupportedLegacy))
		return finishDNSProjection(result, nil, false)
	}
	changed := false
	if raw, present := root["dns"]; present {
		var section dnsObject
		if json.Unmarshal(raw, &section) != nil || section == nil {
			result.Findings = append(result.Findings, dnsFailure("dns", "invalid_section", "DNS must be a JSON object.", diagnostics.ManualRequired))
			return finishDNSProjection(result, nil, false)
		}
		var servers []dnsObject
		if value, present := section["servers"]; present && (json.Unmarshal(value, &servers) != nil || !bytes.HasPrefix(bytes.TrimSpace(value), []byte("["))) {
			result.Findings = append(result.Findings, dnsFailure("dns.servers", "invalid_dns_servers", "DNS servers must be an array of typed objects.", diagnostics.ManualRequired))
			return finishDNSProjection(result, nil, false)
		}
		var rules []dnsObject
		if value, present := section["rules"]; present && (json.Unmarshal(value, &rules) != nil || !bytes.HasPrefix(bytes.TrimSpace(value), []byte("["))) {
			result.Findings = append(result.Findings, dnsFailure("dns.rules", "invalid_rule_array", "DNS rules must be an array.", diagnostics.ManualRequired))
			return finishDNSProjection(result, nil, false)
		}
		if _, present := section["independent_cache"]; present {
			delete(section, "independent_cache")
			changed = true
			result.Findings = append(result.Findings, dnsWarning("dns.independent_cache", "dns_cache_partition_changed", "DNS caches are always separated by transport in sing-box 1.14. The obsolete flag was removed."))
		}
		fakeRaw, hasFake := section["fakeip"]
		var fake dnsObject
		_ = json.Unmarshal(fakeRaw, &fake)
		fakeUsed := false
		rcodes := map[string]string{}
		retained := make([]dnsObject, 0, len(servers))
		seenTags := map[string]bool{}
		for i, server := range servers {
			prefix := fmt.Sprintf("dns.servers[%d]", i)
			if server == nil {
				result.Findings = append(result.Findings, dnsFailure(prefix, "invalid_dns_server", "DNS server must be an object.", diagnostics.ManualRequired))
				continue
			}
			kind := dnsString(server, "type")
			if tag := dnsString(server, "tag"); tag != "" {
				if seenTags[tag] {
					result.Findings = append(result.Findings, dnsFailure(prefix+".tag", "dns_server_tag_duplicate", "DNS server tags must be unique; correct the duplicate before migration.", diagnostics.ManualRequired))
				}
				seenTags[tag] = true
			}
			_, legacyAddress := server["address"]
			if kind != "" && kind != "legacy" && legacyAddress {
				result.Findings = append(result.Findings, dnsFailure(prefix, "dns_representation_conflict", "Choose either the legacy address or the typed server fields before migration.", diagnostics.ManualRequired))
			} else if kind == "" || kind == "legacy" {
				address := dnsString(server, "address")
				if strings.HasPrefix(address, "rcode://") {
					code := map[string]string{"rcode://success": "NOERROR", "rcode://name_error": "NXDOMAIN", "rcode://refused": "REFUSED"}[address]
					tag := dnsString(server, "tag")
					if code == "" || tag == "" || !rcodeReferencesSafe(root, section, servers, rules, tag) || hasDNSExtraFields(server, "type", "tag", "address") {
						result.Findings = append(result.Findings, dnsFailure(prefix, "dns_rcode_manual", "Review the pseudo-server references and replace them with explicit predefined DNS responses.", diagnostics.ManualRequired))
					} else {
						rcodes[tag] = code
						changed = true
						result.Findings = append(result.Findings, dnsWarning(prefix, "dns_rcode_migrated", "The fully resolved DNS pseudo-server was replaced by equivalent predefined responses."))
						continue
					}
				} else if address == "fakeip" {
					if !hasFake || !dnsBool(fake, "enabled") || hasDNSExtraFields(server, "type", "tag", "address") {
						result.Findings = append(result.Findings, dnsFailure(prefix, "dns_fakeip_manual", "An active FakeIP server requires unambiguous preserved ranges and references.", diagnostics.ManualRequired))
					} else {
						server = dnsObject{"type": dnsJSON("fakeip"), "tag": server["tag"]}
						for _, key := range []string{"inet4_range", "inet6_range"} {
							if value, present := fake[key]; present {
								server[key] = value
							}
						}
						fakeUsed = true
						changed = true
						result.Findings = append(result.Findings, dnsWarning(prefix, "dns_fakeip_migrated", "FakeIP ranges were moved to the typed server while preserving its tag."))
					}
				} else {
					converted, finding := convertLegacyDNSServer(root, servers, server, prefix)
					if finding != nil {
						result.Findings = append(result.Findings, *finding)
					} else {
						server = converted
						changed = true
						result.Findings = append(result.Findings, dnsWarning(prefix, "dns_server_migrated", "The typed DNS transport preserves the captured address, bootstrap resolver and dial path."))
					}
				}
			}
			if dnsString(server, "type") == "predefined" {
				result.Findings = append(result.Findings, dnsFailure(prefix, "dns_predefined_server_removed", "Predefined responses belong to DNS rules; this historical server requires correction.", diagnostics.ManualRequired))
			}
			if dnsString(server, "type") == "local" {
				if historical && localDNSPolicyConstrained(rules, dnsString(server, "tag")) {
					result.Findings = append(result.Findings, dnsFailure(prefix, "dns_local_policy_changed", "Choose an explicit unicast resolver or accept the new local and link-local multicast behavior.", diagnostics.ManualRequired))
				} else {
					result.Findings = append(result.Findings, dnsWarning(prefix, "dns_local_behavior_changed", "Local DNS uses the current system resolver and may use multicast for local and link-local names. Review local search and preferred-domain behavior."))
				}
			}
			if host := dnsString(server, "server"); host != "" && strings.Contains(host, ":") {
				if _, err := netip.ParseAddr(host); err != nil {
					result.Findings = append(result.Findings, dnsFailure(prefix+".server", "dns_host_port_ambiguous", "The typed server host must exclude its port. Recover the intended host and port explicitly.", diagnostics.ManualRequired))
				}
			}
			retained = append(retained, server)
		}
		if hasFake {
			if fakeUsed && hasDNSExtraFields(fake, "enabled", "inet4_range", "inet6_range") {
				result.Findings = append(result.Findings, dnsFailure("dns.fakeip", "dns_fakeip_manual", "Unknown legacy FakeIP fields must be corrected explicitly before migration.", diagnostics.ManualRequired))
			}
			inactive := !activeDNSRaw(fakeRaw) || (fake != nil && !dnsBool(fake, "enabled") && !hasDNSExtraFields(fake, "enabled", "inet4_range", "inet6_range"))
			if !fakeUsed && !inactive {
				result.Findings = append(result.Findings, dnsFailure("dns.fakeip", "dns_fakeip_manual", "Active or ambiguous legacy FakeIP ranges must be bound to a preserved typed server.", diagnostics.ManualRequired))
			} else {
				delete(section, "fakeip")
				changed = true
				result.Findings = append(result.Findings, dnsWarning("dns.fakeip", "dns_fakeip_key_removed", "The removed legacy FakeIP key was cleared after checking its active server usage."))
			}
		}
		for _, rule := range rules {
			if code, found := rcodes[dnsString(rule, "server")]; found {
				delete(rule, "server")
				rule["action"] = dnsJSON("predefined")
				rule["rcode"] = dnsJSON(code)
			}
		}
		if len(retained) > 0 {
			final := dnsString(section, "final")
			for i, server := range retained {
				if dnsString(server, "type") == "fakeip" && (final == dnsString(server, "tag") || (final == "" && i == 0)) {
					result.Findings = append(result.Findings, dnsFailure("dns.final", "dns_fakeip_default_manual", "FakeIP cannot be the default DNS transport. Choose an existing normal resolver explicitly while preserving FakeIP rule references.", diagnostics.ManualRequired))
				}
			}
		}
		if changed {
			section["servers"] = dnsJSON(retained)
			if _, present := section["rules"]; present {
				section["rules"] = dnsJSON(rules)
			}
		}
		if changed {
			root["dns"] = dnsJSON(section)
		}
		result.Findings = append(result.Findings, analyzeDNSRules(root, rules, historical)...)
		result.Findings = append(result.Findings, analyzeDefaultResolver(root, retained)...)
		if historical {
			result.Findings = append(result.Findings, analyzeInternalDNSReachability(root, rules)...)
		}
	}
	if cacheRaw, present := root["experimental"]; present {
		var experimental dnsObject
		_ = json.Unmarshal(cacheRaw, &experimental)
		var cache dnsObject
		_ = json.Unmarshal(experimental["cache_file"], &cache)
		if dnsBool(cache, "store_dns") && activeDNSRaw(cache["rdrc_timeout"]) {
			result.Findings = append(result.Findings, dnsFailure("experimental.cache_file.rdrc_timeout", "dns_cache_privacy_choice", "Review and remove the legacy rejection-cache timeout before choosing full DNS persistence.", diagnostics.ManualRequired))
		}
		if raw, present := cache["store_rdrc"]; present {
			if _, dual := cache["store_dns"]; dual && (dnsBool(cache, "store_dns") || dnsBool(cache, "store_rdrc") || activeDNSRaw(cache["rdrc_timeout"])) {
				result.Findings = append(result.Findings, dnsFailure("experimental.cache_file", "dns_cache_privacy_choice", "Choose rejection-only or full DNS persistence explicitly; remove the conflicting legacy option and review retention.", diagnostics.ManualRequired))
			} else if bytes.Equal(bytes.TrimSpace(raw), []byte("false")) {
				delete(cache, "store_rdrc")
				experimental["cache_file"] = dnsJSON(cache)
				root["experimental"] = dnsJSON(experimental)
				changed = true
			} else if dnsBool(cache, "store_rdrc") {
				result.Findings = append(result.Findings, dnsWarning("experimental.cache_file.store_rdrc", "dns_rejection_cache_retained", "Rejection-only DNS persistence remains enabled. Full DNS cache persistence requires an explicit privacy choice."))
			}
		}
	}
	return finishDNSProjection(result, root, changed)
}

func finishDNSProjection(result DNSCompatibility, root dnsObject, changed bool) (DNSCompatibility, error) {
	if err := diagnostics.FirstError(result.Findings); err != nil {
		result.Outcome = diagnostics.ManualRequired
		for _, finding := range result.Findings {
			if finding.Severity == diagnostics.Error && finding.MigrationOutcome == diagnostics.UnsupportedLegacy {
				result.Outcome = diagnostics.UnsupportedLegacy
				break
			}
		}
		return result, err
	}
	if len(result.Findings) > 0 {
		result.Outcome = diagnostics.AutomaticDiagnostic
	}
	if changed {
		result.Candidate = dnsJSON(root)
	}
	return result, nil
}

func convertLegacyDNSServer(root dnsObject, servers []dnsObject, old dnsObject, path string) (dnsObject, *diagnostics.Finding) {
	fail := func(code, message, outcome string) (dnsObject, *diagnostics.Finding) {
		f := dnsFailure(path, code, message, outcome)
		return nil, &f
	}
	if hasDNSExtraFields(old, "type", "tag", "address", "address_resolver", "address_strategy", "strategy", "detour", "address_fallback_delay", "client_subnet") {
		return fail("dns_legacy_fields_unknown", "Review unsupported legacy DNS fields before choosing a typed server.", diagnostics.ManualRequired)
	}
	for _, key := range []string{"type", "tag", "address", "address_resolver", "address_strategy", "strategy", "detour", "client_subnet"} {
		if raw, present := old[key]; present {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return fail("dns_legacy_fields_unknown", "Correct the legacy DNS field types before choosing a typed transport.", diagnostics.ManualRequired)
			}
		}
	}
	if strategy := dnsString(old, "strategy"); (strategy != "" && strategy != "as_is") || activeDNSRaw(old["client_subnet"]) || activeDNSRaw(old["address_fallback_delay"]) {
		return fail("dns_legacy_strategy_manual", "Select explicit answer strategy, subnet or fallback behavior; these legacy per-server semantics cannot be dropped.", diagnostics.ManualRequired)
	}
	address := dnsString(old, "address")
	if address == "local" || strings.HasPrefix(address, "dhcp://") {
		return fail("dns_legacy_local_manual", "Choose the typed system or DHCP resolver and its intended routing behavior explicitly.", diagnostics.ManualRequired)
	}
	kind := "udp"
	source := address
	if strings.Contains(source, "://") {
		kind, _, _ = strings.Cut(source, "://")
	} else {
		source = "udp://" + source
	}
	if !strings.Contains("|udp|tcp|tls|https|h3|quic|", "|"+kind+"|") {
		return fail("dns_legacy_transport_unsupported", "The legacy transport has no supported automatic replacement.", diagnostics.UnsupportedLegacy)
	}
	u, err := url.Parse(source)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fail("dns_legacy_address_manual", "Correct the legacy DNS transport address before migration.", diagnostics.ManualRequired)
	}
	if kind != "https" && kind != "h3" && u.Path != "" {
		return fail("dns_legacy_address_manual", "This DNS transport does not have a path.", diagnostics.ManualRequired)
	}
	converted := dnsObject{"type": dnsJSON(kind)}
	if tag, present := old["tag"]; present {
		converted["tag"] = tag
	}
	host := u.Hostname()
	converted["server"] = dnsJSON(host)
	if port := u.Port(); port != "" {
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil || value == 0 {
			return fail("dns_legacy_address_manual", "Correct the DNS server port before migration.", diagnostics.ManualRequired)
		}
		converted["server_port"] = dnsJSON(value)
	}
	if kind == "https" || kind == "h3" {
		if u.Path != "" {
			converted["path"] = dnsJSON(u.Path)
		}
	}
	detour := dnsString(old, "detour")
	if detour == "" {
		detour = defaultOutboundTag(root)
	}
	if detour == "" {
		return fail("dns_legacy_detour_manual", "Select the old effective outbound explicitly; its default identity is unavailable.", diagnostics.ManualRequired)
	}
	outbound, found := findDNSOutbound(root, detour)
	if !found {
		return fail("dns_legacy_detour_manual", "The referenced DNS outbound is unavailable; correct the reference before migration.", diagnostics.ManualRequired)
	}
	// The pinned ordinary dialer rejects an empty direct detour. Typed DNS uses
	// a direct dialer by default; custom direct outbounds retain their detour.
	if dnsString(outbound, "type") != "direct" || hasDNSExtraFields(outbound, "type", "tag") {
		converted["detour"] = dnsJSON(detour)
	}
	resolver := dnsString(old, "address_resolver")
	if resolver != "" {
		found := false
		for _, server := range servers {
			if dnsString(server, "tag") == resolver && resolver != dnsString(old, "tag") {
				found = true
			}
		}
		if !found {
			return fail("dns_bootstrap_reference_manual", "Choose an existing nonrecursive bootstrap resolver.", diagnostics.ManualRequired)
		}
		options := dnsObject{"server": dnsJSON(resolver)}
		if strategy, present := old["address_strategy"]; present {
			options["strategy"] = strategy
		}
		converted["domain_resolver"] = dnsJSON(options)
	} else if _, err := netip.ParseAddr(host); err != nil || activeDNSRaw(old["address_strategy"]) {
		return fail("dns_bootstrap_reference_manual", "A domain-based legacy server needs an explicit preserved bootstrap resolver and strategy.", diagnostics.ManualRequired)
	}
	return converted, nil
}

func defaultOutboundTag(root dnsObject) string {
	var route dnsObject
	_ = json.Unmarshal(root["route"], &route)
	if tag := dnsString(route, "final"); tag != "" {
		return tag
	}
	var rows []dnsObject
	_ = json.Unmarshal(root["outbounds"], &rows)
	if len(rows) > 0 {
		return dnsString(rows[0], "tag")
	}
	return ""
}
func findDNSOutbound(root dnsObject, tag string) (dnsObject, bool) {
	var rows []dnsObject
	_ = json.Unmarshal(root["outbounds"], &rows)
	for _, row := range rows {
		if dnsString(row, "tag") == tag {
			return row, true
		}
	}
	return nil, false
}
func dnsString(object dnsObject, key string) string {
	var value string
	_ = json.Unmarshal(object[key], &value)
	return value
}
func dnsBool(object dnsObject, key string) bool {
	var value bool
	_ = json.Unmarshal(object[key], &value)
	return value
}
func dnsJSON(value any) json.RawMessage { data, _ := json.Marshal(value); return data }
func hasDNSExtraFields(object dnsObject, allowed ...string) bool {
	for key := range object {
		found := false
		for _, field := range allowed {
			if field == key {
				found = true
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}
func activeDNSRaw(raw json.RawMessage) bool {
	trimmed := string(bytes.TrimSpace(raw))
	return trimmed != "" && trimmed != "null" && trimmed != "false" && trimmed != "0" && trimmed != "\"\"" && trimmed != "[]" && trimmed != "{}"
}
func dnsWarning(path, code, message string) diagnostics.Finding {
	return diagnostics.Finding{Kind: "dns", Path: path, Code: code, Severity: diagnostics.Warn, Message: message, MigrationOutcome: diagnostics.AutomaticDiagnostic, AutomaticAvailable: true}
}
func dnsFailure(path, code, message, outcome string) diagnostics.Finding {
	return diagnostics.Finding{Kind: "dns", Path: path, Code: code, Severity: diagnostics.Error, Message: message, MigrationOutcome: outcome, OperatorActionRequired: true}
}

func rcodeReferencesSafe(root, section dnsObject, servers, rules []dnsObject, tag string) bool {
	if dnsString(section, "final") == tag {
		return false
	}
	for _, server := range servers {
		if dnsString(server, "address_resolver") == tag || dnsString(server, "domain_resolver") == tag {
			return false
		}
		var resolver dnsObject
		_ = json.Unmarshal(server["domain_resolver"], &resolver)
		if dnsString(resolver, "server") == tag {
			return false
		}
	}
	var route dnsObject
	_ = json.Unmarshal(root["route"], &route)
	if bytes.Contains(route["default_domain_resolver"], dnsJSON(tag)) {
		return false
	}
	for _, rule := range rules {
		if children, present := rule["rules"]; present && bytes.Contains(children, dnsJSON(tag)) {
			return false
		}
		if dnsString(rule, "server") == tag {
			action := dnsString(rule, "action")
			if action != "" && action != "route" {
				return false
			}
			for _, key := range []string{"strategy", "rewrite_ttl", "client_subnet", "disable_cache", "race", "speculative"} {
				if activeDNSRaw(rule[key]) {
					return false
				}
			}
		}
	}
	for _, kind := range []string{"outbounds", "endpoints", "inbounds", "services"} {
		if bytes.Contains(root[kind], dnsJSON(tag)) {
			return false
		}
	}
	return true
}

func localDNSPolicyConstrained(rules []dnsObject, tag string) bool {
	// Inspection itself must remain bounded, before official recursive decoding.
	for visited := 0; len(rules) > 0; visited++ {
		if visited >= rulepolicy.MaxNodes {
			return true
		}
		rule := rules[0]
		rules = rules[1:]
		if children, present := rule["rules"]; present {
			var nested []dnsObject
			_ = json.Unmarshal(children, &nested)
			if dnsString(rule, "server") == tag || dnsString(rule, "server") == "" {
				rules = append(rules, nested...)
			}
		}
		if server := dnsString(rule, "server"); server != "" && server != tag {
			continue
		}
		for _, key := range []string{"domain", "domain_suffix", "domain_regex", "domain_keyword"} {
			var values []string
			if json.Unmarshal(rule[key], &values) != nil {
				var single string
				if json.Unmarshal(rule[key], &single) == nil {
					values = []string{single}
				}
			}
			for _, value := range values {
				value = strings.ToLower(strings.TrimSuffix(value, "."))
				if value == "local" || strings.HasSuffix(value, ".local") || strings.Contains(value, "in-addr.arpa") || strings.Contains(value, "ip6.arpa") {
					return true
				}
			}
		}
	}
	return false
}

func analyzeDefaultResolver(root dnsObject, servers []dnsObject) []diagnostics.Finding {
	var findings []diagnostics.Finding
	for _, kind := range []string{"outbounds", "endpoints"} {
		var rows []dnsObject
		_ = json.Unmarshal(root[kind], &rows)
		for i, row := range rows {
			if activeDNSRaw(row["domain_strategy"]) {
				findings = append(findings, dnsWarning(fmt.Sprintf("%s[%d].domain_strategy", kind, i), "dns_legacy_domain_strategy_retained", "The supported legacy dialer strategy remains intact. Review an explicit resolver before replacing it."))
			}
			if _, err := netip.ParseAddr(dnsString(row, "server")); err != nil && dnsString(row, "server") != "" && len(servers) > 1 && !activeDNSRaw(row["domain_resolver"]) {
				var route dnsObject
				_ = json.Unmarshal(root["route"], &route)
				if !activeDNSRaw(route["default_domain_resolver"]) && !activeDNSRaw(row["domain_strategy"]) {
					findings = append(findings, dnsFailure(fmt.Sprintf("%s[%d].domain_resolver", kind, i), "dns_default_resolver_manual", "Choose the bootstrap resolver explicitly for this domain-based dialer; multiple DNS servers make an inferred choice unsafe.", diagnostics.ManualRequired))
				}
			}
		}
	}
	return findings
}
