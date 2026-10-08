package singboxconfig

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/rulepolicy"
	"github.com/sagernet/sing/common/json/badoption"
)

type LegacyOptionsProjection struct {
	Candidate []byte
	Findings  []diagnostics.Finding
}

// PrepareBaseOptionsUpgrade owns base/rule compatibility only. Listen, TLS and
// protocol transformations are delegated to their corresponding entity owners.
func PrepareBaseOptionsUpgrade(source []byte) (LegacyOptionsProjection, error) {
	return prepareBaseOptionsUpgrade(source, true)
}

func prepareBaseOptionsUpgrade(source []byte, storedBase bool) (LegacyOptionsProjection, error) {
	result := LegacyOptionsProjection{Candidate: bytes.Clone(source)}
	var root map[string]json.RawMessage
	if json.Unmarshal(source, &root) != nil || root == nil {
		return result, fmt.Errorf("base configuration must be an object")
	}
	changed := false
	if raw, present := root["certificate_providers"]; present && storedBase {
		var providers []json.RawMessage
		if json.Unmarshal(raw, &providers) != nil || len(providers) > 0 {
			f := baseLegacyFinding("certificate_providers", "TLS_PROVIDER_STORE_CONFLICT", false)
			f.Kind = "tls"
			f.MigrationOutcome = diagnostics.ManualRequired
			f.Message = "Provider definitions belong to the encrypted TLS store. Move this base-owned representation through the TLS owner before retrying; no provider source is silently preferred."
			result.Findings = append(result.Findings, f)
		} else {
			delete(root, "certificate_providers")
			changed = true
		}
	}
	remove := func(fields map[string]json.RawMessage, key, path string, accepted bool, code string) {
		if _, present := fields[key]; !present {
			return
		}
		f := baseLegacyFinding(path+"."+key, code, accepted)
		result.Findings = append(result.Findings, f)
		if accepted {
			delete(fields, key)
			changed = true
		}
	}
	if raw, present := root["network_namespaces"]; present {
		var holders []json.RawMessage
		if json.Unmarshal(raw, &holders) != nil || len(holders) > 0 {
			result.Findings = append(result.Findings, baseLegacyFinding("network_namespaces", "NAMESPACE_HOLDER_UNAVAILABLE", false))
		}
	}
	var debug map[string]json.RawMessage
	if json.Unmarshal(root["debug"], &debug) == nil {
		if raw, present := debug["oom_killer"]; present {
			remove(debug, "oom_killer", "debug", bytes.Equal(bytes.TrimSpace(raw), []byte("null")), "DEBUG_OOM_LEGACY_UNSUPPORTED")
		}
		root["debug"], _ = json.Marshal(debug)
	}
	var experimental map[string]json.RawMessage
	if json.Unmarshal(root["experimental"], &experimental) == nil {
		var debug map[string]json.RawMessage
		if json.Unmarshal(experimental["debug"], &debug) == nil {
			if raw, present := debug["oom_killer"]; present {
				remove(debug, "oom_killer", "experimental.debug", bytes.Equal(bytes.TrimSpace(raw), []byte("null")), "DEBUG_OOM_LEGACY_UNSUPPORTED")
			}
			experimental["debug"], _ = json.Marshal(debug)
		}
		var clash map[string]json.RawMessage
		if json.Unmarshal(experimental["clash_api"], &clash) == nil {
			for _, key := range []string{"cache_file", "cache_id", "store_mode", "store_selected", "store_fakeip"} {
				if raw, present := clash[key]; present {
					remove(clash, key, "experimental.clash_api", emptyBaseLegacy(key, raw), "CLASH_CACHE_EXPLICIT_CHOICE_REQUIRED")
				}
			}
			experimental["clash_api"], _ = json.Marshal(clash)
		}
		root["experimental"], _ = json.Marshal(experimental)
	}
	var walkRules func([]json.RawMessage, string, int) []json.RawMessage
	nodes := 0
	walkRules = func(rules []json.RawMessage, path string, depth int) []json.RawMessage {
		if depth > rulepolicy.MaxDepth || nodes+len(rules) > rulepolicy.MaxNodes {
			result.Findings = append(result.Findings, baseLegacyFinding(path, "RULE_DEPTH_EXCEEDED", false))
			return rules
		}
		nodes += len(rules)
		for i, raw := range rules {
			var rule map[string]json.RawMessage
			if json.Unmarshal(raw, &rule) != nil {
				continue
			}
			prefix := fmt.Sprintf("%s[%d]", path, i)
			for _, key := range []string{"geosite", "geoip", "source_geoip"} {
				if raw, present := rule[key]; present {
					remove(rule, key, prefix, emptyBaseLegacy(key, raw), "RULE_REMOVED_CONDITION")
				}
			}
			if raw, present := rule["rule_set_ipcidr_match_source"]; present {
				var legacy bool
				accepted := json.Unmarshal(raw, &legacy) == nil
				if current, present := rule["rule_set_ip_cidr_match_source"]; present {
					var value bool
					accepted = accepted && json.Unmarshal(current, &value) == nil && value == legacy
				}
				f := baseLegacyFinding(prefix+".rule_set_ipcidr_match_source", "RULE_SOURCE_MATCH_ALIAS_NORMALIZED", accepted)
				if !accepted {
					f.Code, f.MigrationOutcome = "RULE_SOURCE_MATCH_ALIAS_CONFLICT", diagnostics.ManualRequired
					f.Message = "Select one matching current source-IP rule-set value explicitly before retrying. Divergent aliases are preserved."
				} else {
					if _, present := rule["rule_set_ip_cidr_match_source"]; !present && legacy {
						rule["rule_set_ip_cidr_match_source"] = raw
					}
					delete(rule, "rule_set_ipcidr_match_source")
					changed = true
				}
				result.Findings = append(result.Findings, f)
			}
			// Raw TCP spoof activation is not authorized by option registration.
			for _, key := range []string{"tls_spoof", "tls_spoof_method"} {
				if raw, present := rule[key]; present && !emptyBaseLegacy(key, raw) {
					result.Findings = append(result.Findings, baseLegacyFinding(prefix+"."+key, "TLS_SPOOF_UNAVAILABLE", false))
				}
			}
			var nested []json.RawMessage
			if json.Unmarshal(rule["rules"], &nested) == nil {
				rule["rules"], _ = json.Marshal(walkRules(nested, prefix+".rules", depth+1))
			}
			rules[i], _ = json.Marshal(rule)
		}
		return rules
	}
	for _, section := range []string{"dns", "route"} {
		nodes = 0
		var fields map[string]json.RawMessage
		if json.Unmarshal(root[section], &fields) != nil {
			continue
		}
		if section == "route" {
			for _, key := range []string{"geoip", "geosite"} {
				if raw, present := fields[key]; present {
					remove(fields, key, section, bytes.Equal(bytes.TrimSpace(raw), []byte("null")), "GEO_DATABASE_REMOVED")
				}
			}
		}
		var rules []json.RawMessage
		if json.Unmarshal(fields["rules"], &rules) == nil {
			fields["rules"], _ = json.Marshal(walkRules(rules, section+".rules", 0))
		}
		root[section], _ = json.Marshal(fields)
	}
	if err := diagnostics.FirstError(result.Findings); err != nil {
		return result, err
	}
	if changed {
		result.Candidate, _ = json.Marshal(root)
	}
	return result, nil
}

func emptyBaseLegacy(key string, raw []byte) bool {
	switch key {
	case "store_mode", "store_selected", "store_fakeip", "rule_set_ipcidr_match_source":
		var value bool
		return json.Unmarshal(raw, &value) == nil && !value
	case "geoip", "geosite", "source_geoip":
		var value badoption.Listable[string]
		return json.Unmarshal(raw, &value) == nil && len(value) == 0
	default:
		var value string
		return json.Unmarshal(raw, &value) == nil && value == ""
	}
}

func baseLegacyFinding(path, code string, accepted bool) diagnostics.Finding {
	f := diagnostics.Finding{Kind: "config", Path: path, Code: code, Severity: diagnostics.Warn, Message: "An empty compatibility field was removed without changing its consumer's behavior.", MigrationOutcome: diagnostics.AutomaticDiagnostic, AutomaticAvailable: true}
	if !accepted {
		f.Severity = diagnostics.Error
		f.Message = "This option has no authorized automatic replacement. Correct the named field explicitly before retrying; the source remains recoverable."
		f.MigrationOutcome = diagnostics.UnsupportedLegacy
		f.AutomaticAvailable = false
		f.OperatorActionRequired = true
	}
	if !accepted && code == "CLASH_CACHE_EXPLICIT_CHOICE_REQUIRED" {
		f.MigrationOutcome = diagnostics.ManualRequired
		f.Message = "Select the current cache owner and its path/privacy policy explicitly before retrying. Cache persistence is not inferred."
	}
	return f
}

func BaseOptionsFindings(source []byte) []diagnostics.Finding {
	// A complete runtime candidate contains providers projected by the TLS
	// owner. The second-store guard applies to base persistence, while runtime
	// provider definitions are validated by the TLS owner and pinned decoder.
	result, _ := prepareBaseOptionsUpgrade(source, false)
	return result.Findings
}
