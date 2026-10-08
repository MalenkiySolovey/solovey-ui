package entitytls

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
)

const LegacyInlineMode = "legacy_inline"
const NativeProviderMode = "native"

// ProviderDefinition is private candidate/runtime material. Public consumers
// must use ProviderViews, which contains masked secret presence instead.
type ProviderDefinition struct {
	Tag         string          `json:"tag"`
	Type        string          `json:"type"`
	RuntimeMode string          `json:"runtimeMode"`
	Options     json.RawMessage `json:"options"`
}

type ProviderUpgrade struct {
	Profiles  []model.Tls
	Providers []ProviderDefinition
	Findings  []diagnostics.Finding
}

// PrepareProviderUpgrade does not write storage or open files. Legacy ACME
// stays on its accepted inline runtime adapter: the new native provider HTTP
// transport is not assumed equivalent to certmagic's old system transport.
func PrepareProviderUpgrade(source []model.Tls, existing []ProviderDefinition) (ProviderUpgrade, error) {
	result := ProviderUpgrade{Profiles: append([]model.Tls(nil), source...), Providers: append([]ProviderDefinition(nil), existing...)}
	byTag := map[string]ProviderDefinition{}
	for i, provider := range existing {
		if _, duplicate := byTag[provider.Tag]; duplicate || provider.Tag == "" {
			result.Findings = append(result.Findings, providerFinding(fmt.Sprintf("certificate_providers[%d].tag", i), "TLS_PROVIDER_TAG_CONFLICT", diagnostics.Error, "Provider identity is missing or duplicated."))
		}
		byTag[provider.Tag] = provider
	}
	indices := make([]int, len(source))
	for i := range indices {
		indices[i] = i
	}
	sort.SliceStable(indices, func(i, j int) bool { return source[indices[i]].Id < source[indices[j]].Id })
	for _, index := range indices {
		row := source[index]
		if row.Id == 0 {
			continue
		}
		path := fmt.Sprintf("tls[%d].server", row.Id)
		var server map[string]json.RawMessage
		if json.Unmarshal(row.Server, &server) != nil || server == nil {
			result.Findings = append(result.Findings, providerFinding(path, "TLS_SERVER_INVALID", diagnostics.Error, "TLS server must be an object."))
			continue
		}
		acme, hasACME := server["acme"]
		reference, hasProvider := server["certificate_provider"]
		if !hasACME && !hasProvider {
			continue
		}
		var options map[string]json.RawMessage
		mode := LegacyInlineMode
		if hasACME {
			if json.Unmarshal(acme, &options) != nil || options == nil {
				result.Findings = append(result.Findings, providerFinding(path+".acme", "TLS_ACME_INVALID", diagnostics.Error, "ACME options must be an object."))
				continue
			}
		}
		var tag string
		if hasProvider && json.Unmarshal(reference, &tag) == nil {
			provider, found := byTag[tag]
			if !found {
				result.Findings = append(result.Findings, providerFinding(path+".certificate_provider", "TLS_PROVIDER_REFERENCE_MISSING", diagnostics.Error, "The referenced provider is missing. Restore it or select an available provider."))
				continue
			}
			if hasACME && (provider.Type != "acme" || provider.RuntimeMode != LegacyInlineMode || !providerJSONEqual(acme, provider.Options)) {
				result.Findings = append(result.Findings, providerFinding(path+".acme", "TLS_PROVIDER_CONFLICT", diagnostics.Error, "Inline ACME and the referenced provider differ. Choose one source before retrying."))
				continue
			}
			if !hasACME {
				continue
			}
		} else if hasProvider {
			if hasACME {
				result.Findings = append(result.Findings, providerFinding(path+".acme", "TLS_PROVIDER_CONFLICT", diagnostics.Error, "Inline ACME and an inline provider cannot be silently preferred."))
				continue
			}
			var inline map[string]json.RawMessage
			if json.Unmarshal(reference, &inline) != nil || inline == nil {
				result.Findings = append(result.Findings, providerFinding(path+".certificate_provider", "TLS_PROVIDER_INVALID", diagnostics.Error, "Provider must be a reference or object."))
				continue
			}
			var kind string
			_ = json.Unmarshal(inline["type"], &kind)
			if kind != "acme" {
				result.Findings = append(result.Findings, providerFinding(path+".certificate_provider.type", "TLS_PROVIDER_UNAVAILABLE", diagnostics.Error, "This provider lacks the complete product authorization contract."))
				continue
			}
			delete(inline, "type")
			options = inline
			mode = NativeProviderMode
		}
		if tag == "" {
			encoded, _ := json.Marshal(options)
			base := fmt.Sprintf("tls-acme-%d", row.Id)
			tag = base
			for suffix := 2; ; suffix++ {
				occupied, present := byTag[tag]
				if !present {
					break
				}
				if occupied.Type == "acme" && occupied.RuntimeMode == mode && providerJSONEqual(occupied.Options, encoded) {
					break
				}
				tag = fmt.Sprintf("%s-%d", base, suffix)
			}
			if _, found := byTag[tag]; !found {
				definition := ProviderDefinition{Tag: tag, Type: "acme", RuntimeMode: mode, Options: encoded}
				byTag[tag] = definition
				result.Providers = append(result.Providers, definition)
			}
		}
		delete(server, "acme")
		server["certificate_provider"], _ = json.Marshal(tag)
		result.Profiles[index].Server, _ = json.Marshal(server)
		code, message := "TLS_PROVIDER_EXTRACTED", "Provider options were moved into the TLS collection without changing current native intent."
		if mode == LegacyInlineMode {
			code = "TLS_ACME_COMPATIBILITY_PRESERVED"
			message = "ACME is stored once in the TLS collection. Its accepted legacy runtime adapter preserves HTTP, DNS, account and directory semantics."
		}
		result.Findings = append(result.Findings, providerFinding(path+".certificate_provider", code, diagnostics.Warn, message))
	}
	if err := diagnostics.FirstError(result.Findings); err != nil {
		result.Profiles = append([]model.Tls(nil), source...)
		result.Providers = append([]ProviderDefinition(nil), existing...)
		return result, err
	}
	return result, nil
}

func providerJSONEqual(a, b []byte) bool {
	var left, right any
	d := json.NewDecoder(bytes.NewReader(a))
	d.UseNumber()
	if d.Decode(&left) != nil {
		return false
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if d.Decode(&right) != nil {
		return false
	}
	x, _ := json.Marshal(left)
	y, _ := json.Marshal(right)
	return bytes.Equal(x, y)
}

func providerFinding(path, code, severity, message string) diagnostics.Finding {
	outcome := diagnostics.AutomaticDiagnostic
	if severity == diagnostics.Error {
		outcome = diagnostics.ManualRequired
	}
	return diagnostics.Finding{Kind: "tls", Path: path, Code: code, Severity: severity, Message: message, MigrationOutcome: outcome, AutomaticAvailable: severity != diagnostics.Error, OperatorActionRequired: severity == diagnostics.Error}
}
