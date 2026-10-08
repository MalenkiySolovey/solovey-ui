package outbounds

import (
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
)

func PrepareOptionsUpgrade(kind, path string, source json.RawMessage) (json.RawMessage, []diagnostics.Finding, error) {
	if kind == "dns" || kind == "wireguard" || kind == "shadowsocksr" || kind == "ssr" {
		findings := []diagnostics.Finding{{Kind: "outbound", Path: path + ".type", Code: "LEGACY_OUTBOUND_TYPE_UNSUPPORTED", Severity: diagnostics.Error, Message: "This legacy outbound has no accepted current product representation. Correct it through its supported semantic owner before retrying; no protocol substitution is inferred.", MigrationOutcome: diagnostics.UnsupportedLegacy, OperatorActionRequired: true}}
		return source, findings, diagnostics.FirstError(findings)
	}
	if kind != "direct" {
		return source, nil, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(source, &fields) != nil || fields == nil {
		return source, nil, fmt.Errorf("direct outbound options must be an object")
	}
	findings := []diagnostics.Finding{}
	for _, key := range []string{"override_address", "override_port", "proxy_protocol"} {
		raw, present := fields[key]
		if !present {
			continue
		}
		noop := false
		if key == "proxy_protocol" {
			var value uint8
			noop = json.Unmarshal(raw, &value) == nil
		} else if key == "override_address" {
			var value string
			noop = json.Unmarshal(raw, &value) == nil && value == ""
		} else {
			var value uint16
			noop = json.Unmarshal(raw, &value) == nil && value == 0
		}
		f := diagnostics.Finding{Kind: "outbound", Path: path + "." + key, Code: "DIRECT_OUTBOUND_NOOP_REMOVED", Severity: diagnostics.Warn, Message: "The direct outbound compatibility option is inert and was removed. Direct inbound overrides remain supported.", MigrationOutcome: diagnostics.AutomaticDiagnostic, AutomaticAvailable: true}
		if !noop {
			f.Code = "DIRECT_OUTBOUND_OVERRIDE_REMOVED"
			f.Severity = diagnostics.Error
			f.Message = "This active direct outbound override is removed. Choose an explicit supported route before retrying."
			f.MigrationOutcome = diagnostics.UnsupportedLegacy
			f.AutomaticAvailable = false
			f.OperatorActionRequired = true
		} else {
			delete(fields, key)
		}
		findings = append(findings, f)
	}
	if err := diagnostics.FirstError(findings); err != nil {
		return source, findings, err
	}
	if len(findings) == 0 {
		return source, findings, nil
	}
	candidate, err := json.Marshal(fields)
	return candidate, findings, err
}

func CompatibilityCatalogue() []diagnostics.CompatibilityFact {
	return []diagnostics.CompatibilityFact{
		{ID: "DEP-13", Consumer: "direct outbound override_address/override_port", Classification: "REMOVED", Policy: "Typed zero cleanup only; active outbound overrides require a supported route."},
		{ID: "DEP-13", Consumer: "direct inbound override_address/override_port", Classification: "SUPPORTED", Policy: "Inbound overrides retain their accepted semantics."},
		{ID: "DEP-14", Consumer: "direct outbound proxy_protocol", Classification: "NO_OP", Policy: "Remove valid numeric values with diagnostic; no active proxy-protocol claim."},
		{ID: "DEP-20", Consumer: "legacy dns/wireguard/shadowsocksr outbound types", Classification: "UNSUPPORTED_BY_PRODUCT", Policy: "Preserve source and require correction at a supported owner; no protocol substitution."},
		{ID: "DEP-20", Consumer: "block outbound", Classification: "SUPPORTED", Policy: "Retain accepted block behavior; no mass block-to-reject conversion."},
	}
}
