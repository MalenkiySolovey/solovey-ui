package formats

import (
	"bytes"
	"encoding/json"
	"strings"

	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

type JSONExtensionCompatibility struct {
	Original  []byte                `json:"-"`
	Candidate []byte                `json:"-"`
	Outcome   string                `json:"outcome"`
	Findings  []diagnostics.Finding `json:"findings"`
}

// The subscription owner only adapts its flat storage to core-owner inputs.
// It owns no HTTP/DNS grammar and never fabricates a resolver. The two stable
// outbound identities come from the existing local subscription generator.
func PrepareJSONExtensionUpgrade(extension []byte) (JSONExtensionCompatibility, error) {
	return prepareJSONExtension(extension, true)
}
func CanonicalJSONExtension(extension []byte) (JSONExtensionCompatibility, error) {
	return prepareJSONExtension(extension, false)
}

func prepareJSONExtension(extension []byte, historical bool) (JSONExtensionCompatibility, error) {
	result := JSONExtensionCompatibility{Original: bytes.Clone(extension), Candidate: bytes.Clone(extension), Outcome: diagnostics.LosslessAutomatic}
	if len(bytes.TrimSpace(extension)) == 0 {
		return result, nil
	}
	var stored map[string]json.RawMessage
	if json.Unmarshal(extension, &stored) != nil || stored == nil {
		result.Findings = []diagnostics.Finding{{Kind: "subscription", Path: "subJsonExt", Code: "subscription_template_invalid", Severity: diagnostics.Error, Message: "The subscription extension must be a JSON object.", MigrationOutcome: diagnostics.ManualRequired, OperatorActionRequired: true}}
		return finishExtension(result)
	}
	route := map[string]json.RawMessage{"final": json.RawMessage(`"proxy"`)}
	root := map[string]json.RawMessage{"route": nil, "outbounds": json.RawMessage(`[{"type":"direct","tag":"direct"},{"type":"selector","tag":"proxy","outbounds":["direct"]}]`)}
	for _, key := range []string{"log", "dns", "inbounds", "experimental", "http_clients"} {
		if raw, present := stored[key]; present {
			root[key] = raw
		}
	}
	for _, key := range []string{"rules", "rule_set", "default_domain_resolver", "default_http_client"} {
		if raw, present := stored[key]; present {
			route[key] = raw
		}
	}
	root["route"], _ = json.Marshal(route)
	candidate, _ := json.Marshal(root)
	if historical {
		transformed, err := validation.PrepareRuleUpgrade(candidate)
		result.Findings = append(result.Findings, transformed.Findings...)
		if err != nil {
			return finishExtension(result)
		}
		candidate = transformed.Candidate
	} else {
		findings, err := validation.ValidateRuleConditions(candidate)
		result.Findings = append(result.Findings, findings...)
		if err != nil {
			return finishExtension(result)
		}
	}
	if historical {
		transformed, err := singboxconfig.PrepareHTTPUpgrade(candidate)
		result.Findings = append(result.Findings, transformed.Findings...)
		if err != nil {
			return finishExtension(result)
		}
		candidate = transformed.Candidate
	} else {
		findings, err := singboxconfig.ValidateHTTPConfig(candidate)
		result.Findings = append(result.Findings, findings...)
		if err != nil {
			return finishExtension(result)
		}
	}
	if historical {
		transformed, err := singboxconfig.PrepareDNSUpgrade(candidate)
		result.Findings = append(result.Findings, transformed.Findings...)
		if err != nil {
			return finishExtension(result)
		}
		candidate = transformed.Candidate
	} else {
		findings, err := singboxconfig.ValidateDNSConfig(candidate)
		result.Findings = append(result.Findings, findings...)
		if err != nil {
			return finishExtension(result)
		}
		candidate, err = singboxconfig.CanonicalDNSConfig(candidate)
		if err != nil {
			return finishExtension(result)
		}
	}
	result.Findings = append(result.Findings, entityinbounds.TUNDNSFindings(candidate)...)
	if diagnostics.FirstError(result.Findings) != nil {
		return finishExtension(result)
	}
	_ = json.Unmarshal(candidate, &root)
	_ = json.Unmarshal(root["route"], &route)
	for _, key := range []string{"log", "dns", "inbounds", "experimental", "http_clients"} {
		if raw, present := root[key]; present {
			stored[key] = raw
		}
	}
	for _, key := range []string{"rules", "rule_set", "default_domain_resolver", "default_http_client"} {
		if raw, present := route[key]; present {
			stored[key] = raw
		}
	}
	transformed, _ := json.Marshal(stored)
	var original, updated any
	_ = json.Unmarshal(extension, &original)
	_ = json.Unmarshal(transformed, &updated)
	oldCanonical, _ := json.Marshal(original)
	newCanonical, _ := json.Marshal(updated)
	if !bytes.Equal(oldCanonical, newCanonical) {
		result.Candidate = transformed
	}
	return finishExtension(result)
}

func finishExtension(result JSONExtensionCompatibility) (JSONExtensionCompatibility, error) {
	for i := range result.Findings {
		if !strings.HasPrefix(result.Findings[i].Path, "subJsonExt") {
			result.Findings[i].Path = "subJsonExt." + result.Findings[i].Path
		}
	}
	result.Outcome = diagnostics.Outcome(result.Findings)
	if err := diagnostics.FirstError(result.Findings); err != nil {
		result.Candidate = bytes.Clone(result.Original)
		return result, err
	}
	return result, nil
}
