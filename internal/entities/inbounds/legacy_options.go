package entityinbounds

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
)

type OptionsUpgrade struct {
	Candidate json.RawMessage
	Findings  []diagnostics.Finding
}

// PrepareOptionsUpgrade interprets listen and TUN sentinels at their owner.
// Active removed options have no guessed equivalent in route or DNS rules.
func PrepareOptionsUpgrade(kind, path string, source json.RawMessage) (OptionsUpgrade, error) {
	result := OptionsUpgrade{Candidate: bytes.Clone(source)}
	var fields map[string]json.RawMessage
	if json.Unmarshal(source, &fields) != nil || fields == nil {
		return result, fmt.Errorf("inbound options must be an object")
	}
	keys := []string{"sniff", "sniff_override_destination", "sniff_timeout", "domain_strategy", "udp_disable_domain_unmapping", "proxy_protocol", "proxy_protocol_accept_no_header"}
	if kind == "tun" {
		keys = append(keys, "gso", "inet4_address", "inet6_address", "inet4_route_address", "inet6_route_address", "inet4_route_exclude_address", "inet6_route_exclude_address")
	}
	for _, key := range keys {
		raw, present := fields[key]
		if !present {
			continue
		}
		if emptyListenOption(key, raw) {
			delete(fields, key)
			result.Findings = append(result.Findings, listenFinding(path+"."+key, "INBOUND_REMOVED_EMPTY_OPTION", false))
		} else {
			result.Findings = append(result.Findings, listenFinding(path+"."+key, "INBOUND_REMOVED_ACTIVE_OPTION", true))
		}
	}
	if kind == "tun" {
		if raw, present := fields["endpoint_independent_nat"]; present {
			var value bool
			if json.Unmarshal(raw, &value) != nil {
				return result, fmt.Errorf("TUN compatibility flag must be boolean")
			}
			delete(fields, "endpoint_independent_nat")
			result.Findings = append(result.Findings, listenFinding(path+".endpoint_independent_nat", "TUN_NAT_NOOP_REMOVED", false))
		}
	}
	if err := diagnostics.FirstError(result.Findings); err != nil {
		return result, err
	}
	if len(result.Findings) > 0 {
		result.Candidate, _ = json.Marshal(fields)
	}
	return result, nil
}

func emptyListenOption(key string, raw []byte) bool {
	if key == "sniff_timeout" {
		var value badoption.Duration
		return json.Unmarshal(raw, &value) == nil && value == 0
	}
	if key == "domain_strategy" {
		var value option.DomainStrategy
		return json.Unmarshal(raw, &value) == nil && value == 0
	}
	if strings.HasPrefix(key, "inet") {
		var value badoption.Listable[netip.Prefix]
		return json.Unmarshal(raw, &value) == nil && len(value) == 0
	}
	var value bool
	return json.Unmarshal(raw, &value) == nil && !value
}

func listenFinding(path, code string, failure bool) diagnostics.Finding {
	severity, outcome, message := diagnostics.Warn, diagnostics.AutomaticDiagnostic, "An inert compatibility option was removed without changing listen or TUN behavior."
	if failure {
		severity, outcome, message = diagnostics.Error, diagnostics.UnsupportedLegacy, "This active listen or TUN option is removed by the accepted core. Correct it explicitly before retrying; no replacement rule or platform activation is inferred."
	}
	return diagnostics.Finding{Kind: "inbound", Path: path, Code: code, Severity: severity, Message: message, MigrationOutcome: outcome, AutomaticAvailable: !failure, OperatorActionRequired: failure}
}

func LegacyOptionsFindings(kind, path string, source json.RawMessage) []diagnostics.Finding {
	result, err := PrepareOptionsUpgrade(kind, path, source)
	if err != nil && len(result.Findings) == 0 {
		return []diagnostics.Finding{listenFinding(path, "INBOUND_OPTIONS_INVALID", true)}
	}
	return result.Findings
}

func CompatibilityCatalogue() []diagnostics.CompatibilityFact {
	return []diagnostics.CompatibilityFact{
		{ID: "DEP-12", Consumer: "listen.sniff/sniff_override_destination/sniff_timeout/domain_strategy/udp_disable_domain_unmapping", Classification: "REMOVED", Policy: "Typed zero cleanup only; active intent has no inferred route-action replacement."},
		{ID: "DEP-14", Consumer: "listen.proxy_protocol/proxy_protocol_accept_no_header", Classification: "REMOVED", Policy: "False cleanup only; active listen proxy protocol is unsupported."},
		{ID: "DEP-15", Consumer: "tun.inet4/inet6_address/route_address/route_exclude_address; tun.gso", Classification: "REMOVED", Policy: "Empty aliases and false gso are inert; active legacy values require explicit supported correction."},
		{ID: "DEP-16", Consumer: "tun.endpoint_independent_nat", Classification: "NO_OP", Policy: "Remove the obsolete flag with diagnostic; it grants no NAT or interface-DNS policy."},
	}
}
