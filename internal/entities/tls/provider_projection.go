package entitytls

import (
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
)

type CertificateProjection struct {
	Candidate []byte
	Findings  []diagnostics.Finding
}

// PrepareBaseProviders translates submitted core definitions into the TLS
// collection. The returned base has no second authoritative provider store.
func PrepareBaseProviders(source []byte, stored []ProviderDefinition) ([]byte, []ProviderDefinition, []diagnostics.Finding, error) {
	var root map[string]json.RawMessage
	if json.Unmarshal(source, &root) != nil || root == nil {
		return source, stored, nil, fmt.Errorf("TLS base candidate must be an object")
	}
	raw, present := root["certificate_providers"]
	if !present {
		return source, stored, nil, nil
	}
	definitions := append([]ProviderDefinition(nil), stored...)
	var values []map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return source, stored, nil, fmt.Errorf("TLS providers must be an array")
	}
	var findings []diagnostics.Finding
	byTag := map[string]ProviderDefinition{}
	for _, definition := range stored {
		byTag[definition.Tag] = definition
	}
	seen := map[string]bool{}
	for i, value := range values {
		var tag, kind string
		_ = json.Unmarshal(value["tag"], &tag)
		_ = json.Unmarshal(value["type"], &kind)
		path := fmt.Sprintf("certificate_providers[%d]", i)
		if seen[tag] {
			findings = append(findings, providerFinding(path+".tag", "TLS_PROVIDER_TAG_CONFLICT", diagnostics.Error, "Provider identities must be unique."))
			continue
		}
		seen[tag] = true
		delete(value, "tag")
		delete(value, "type")
		options, _ := json.Marshal(value)
		definition := ProviderDefinition{Tag: tag, Type: kind, RuntimeMode: NativeProviderMode, Options: options}
		if old, found := byTag[tag]; found {
			if old.Type != kind || old.RuntimeMode != NativeProviderMode || !providerJSONEqual(old.Options, options) {
				findings = append(findings, providerFinding(path, "TLS_PROVIDER_CONFLICT", diagnostics.Error, "Core and TLS-owned provider definitions differ. Edit the TLS-owned definition before retrying."))
			}
			continue
		}
		if err := ValidateProviderDefinition(definition, false); err != nil {
			findings = append(findings, providerFinding(path, "TLS_PROVIDER_UNAVAILABLE", diagnostics.Error, "The submitted provider is not supported by the product's pinned schema and authorization contract."))
			continue
		}
		definitions = append(definitions, definition)
		byTag[tag] = definition
	}
	if err := diagnostics.FirstError(findings); err != nil {
		return source, stored, findings, err
	}
	delete(root, "certificate_providers")
	candidate, _ := json.Marshal(root)
	return candidate, definitions, findings, nil
}

// ProjectCertificateProviders builds ephemeral official shapes from TLS-owned
// records. No decrypted provider is returned by normal panel data or Doctor.
// Legacy records use the still-accepted inline adapter and its original system
// HTTP/account behavior; native records use the official shared registry.
func ProjectCertificateProviders(source []byte, definitions []ProviderDefinition) (CertificateProjection, error) {
	result := CertificateProjection{Candidate: append([]byte(nil), source...)}
	var root map[string]json.RawMessage
	if json.Unmarshal(source, &root) != nil || root == nil {
		return result, fmt.Errorf("TLS runtime candidate must be an object")
	}
	base, all, findings, err := PrepareBaseProviders(source, definitions)
	result.Findings = findings
	if err != nil {
		return result, err
	}
	if json.Unmarshal(base, &root) != nil {
		return result, fmt.Errorf("TLS base candidate must be an object")
	}
	byTag := map[string]ProviderDefinition{}
	for _, definition := range all {
		byTag[definition.Tag] = definition
	}
	active := map[string]bool{}
	changed := string(base) != string(source)
	for _, section := range []string{"inbounds", "services"} {
		raw, present := root[section]
		if !present {
			continue
		}
		var entities []map[string]json.RawMessage
		if json.Unmarshal(raw, &entities) != nil {
			return result, fmt.Errorf("TLS runtime entity section must be an array")
		}
		sectionChanged := false
		for i, entity := range entities {
			tlsRaw, present := entity["tls"]
			if !present {
				continue
			}
			path := fmt.Sprintf("%s[%d].tls", section, i)
			var fields map[string]json.RawMessage
			if json.Unmarshal(tlsRaw, &fields) != nil || fields == nil {
				result.Findings = append(result.Findings, providerFinding(path, "TLS_SERVER_INVALID", diagnostics.Error, "TLS options must be an object."))
				continue
			}
			reference, present := fields["certificate_provider"]
			if !present {
				continue
			}
			var tag string
			if json.Unmarshal(reference, &tag) != nil {
				result.Findings = append(result.Findings, providerFinding(path+".certificate_provider", "TLS_PROVIDER_STORE_REQUIRED", diagnostics.Error, "Submit inline provider options to the TLS owner before activation."))
				continue
			}
			definition, found := byTag[tag]
			if !found {
				result.Findings = append(result.Findings, providerFinding(path+".certificate_provider", "TLS_PROVIDER_REFERENCE_MISSING", diagnostics.Error, "The referenced TLS provider is missing. Restore it or correct the reference."))
				continue
			}
			var enabled bool
			_ = json.Unmarshal(fields["enabled"], &enabled)
			if err := ValidateProviderDefinition(definition, enabled); err != nil {
				result.Findings = append(result.Findings, providerFinding(path+".certificate_provider", "TLS_PROVIDER_UNAVAILABLE", diagnostics.Error, "The provider's complete schema, build and product authorization contract is unavailable."))
				continue
			}
			if acme, present := fields["acme"]; present && (definition.RuntimeMode != LegacyInlineMode || !providerJSONEqual(acme, definition.Options)) {
				result.Findings = append(result.Findings, providerFinding(path+".acme", "TLS_PROVIDER_CONFLICT", diagnostics.Error, "Inline ACME and the provider reference differ. Choose one source before activation."))
				continue
			}
			if definition.RuntimeMode == LegacyInlineMode {
				delete(fields, "certificate_provider")
				fields["acme"] = definition.Options
				entity["tls"], _ = json.Marshal(fields)
				sectionChanged = true
				result.Findings = append(result.Findings, providerFinding(path+".certificate_provider", "TLS_ACME_COMPATIBILITY_PRESERVED", diagnostics.Warn, "The accepted legacy runtime adapter retains the original ACME transport and account semantics."))
			} else if enabled {
				active[tag] = true
			}
		}
		if sectionChanged {
			root[section], _ = json.Marshal(entities)
			changed = true
		}
	}
	// A new core definition is an explicit native service even before the first
	// reference is bound; registry construction must validate its capability.
	for _, definition := range all {
		if definition.RuntimeMode != NativeProviderMode {
			continue
		}
		if _, stored := findDefinition(definitions, definition.Tag); !stored {
			active[definition.Tag] = true
		}
	}
	var providers []json.RawMessage
	for _, definition := range all {
		if !active[definition.Tag] {
			continue
		}
		if err := ValidateProviderDefinition(definition, true); err != nil {
			result.Findings = append(result.Findings, providerFinding("certificate_providers", "TLS_PROVIDER_UNAVAILABLE", diagnostics.Error, "A native provider cannot activate in this product/build composition."))
			continue
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(definition.Options, &fields)
		fields["tag"], _ = json.Marshal(definition.Tag)
		fields["type"], _ = json.Marshal(definition.Type)
		raw, _ := json.Marshal(fields)
		providers = append(providers, raw)
	}
	if err := diagnostics.FirstError(result.Findings); err != nil {
		return result, err
	}
	if len(providers) > 0 {
		root["certificate_providers"], _ = json.Marshal(providers)
		changed = true
	}
	if changed {
		result.Candidate, _ = json.Marshal(root)
	}
	return result, nil
}

func findDefinition(definitions []ProviderDefinition, tag string) (ProviderDefinition, bool) {
	for _, definition := range definitions {
		if definition.Tag == tag {
			return definition, true
		}
	}
	return ProviderDefinition{}, false
}
