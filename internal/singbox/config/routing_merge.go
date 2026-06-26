package singboxconfig

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MergeMappedRouting merges migrated route rules / rule sets and DNS
// servers/rules into an existing base sing-box config. Existing operator-owned
// rules are preserved, while tag-based collections stay idempotent.
func MergeMappedRouting(current string, mapped map[string]any) (string, bool, error) {
	if strings.TrimSpace(current) == "" {
		current = DefaultBaseConfig
	}
	cfg := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(current), &cfg); err != nil {
		return "", false, fmt.Errorf("routing: existing config is not valid JSON: %w", err)
	}
	changed := false

	if route, ok := mapped["route"].(map[string]any); ok {
		newRules := toAnySlice(route["rules"])
		newRuleSets := toAnySlice(route["rule_set"])
		if len(newRules) > 0 || len(newRuleSets) > 0 {
			dst := decodeConfigObject(cfg["route"])
			if len(newRules) > 0 {
				dst["rules"] = appendUniqueRules(toAnySlice(dst["rules"]), newRules)
			}
			if len(newRuleSets) > 0 {
				dst["rule_set"] = mergeByTag(toAnySlice(dst["rule_set"]), newRuleSets)
			}
			enc, err := json.Marshal(dst)
			if err != nil {
				return "", false, err
			}
			cfg["route"] = enc
			changed = true
		}
	}

	if dns, ok := mapped["dns"].(map[string]any); ok && len(dns) > 0 {
		newServers := toAnySlice(dns["servers"])
		newDNSRules := toAnySlice(dns["rules"])
		if len(newServers) > 0 || len(newDNSRules) > 0 {
			dst := decodeConfigObject(cfg["dns"])
			if len(newServers) > 0 {
				dst["servers"] = mergeByTag(toAnySlice(dst["servers"]), newServers)
			}
			if len(newDNSRules) > 0 {
				dst["rules"] = appendUniqueRules(toAnySlice(dst["rules"]), newDNSRules)
			}
			for _, k := range []string{"strategy", "final", "client_subnet"} {
				if v, ok := dns[k]; ok {
					if _, exists := dst[k]; !exists {
						dst[k] = v
					}
				}
			}
			enc, err := json.Marshal(dst)
			if err != nil {
				return "", false, err
			}
			cfg["dns"] = enc
			changed = true
		}
	}

	if !changed {
		return "", false, nil
	}
	merged, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", false, err
	}
	return string(merged), true, nil
}

func decodeConfigObject(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func toAnySlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func appendUniqueRules(existing, additions []any) []any {
	seen := map[string]struct{}{}
	for _, e := range existing {
		if key, err := json.Marshal(e); err == nil {
			seen[string(key)] = struct{}{}
		}
	}
	for _, a := range additions {
		key, err := json.Marshal(a)
		if err != nil {
			existing = append(existing, a)
			continue
		}
		if _, dup := seen[string(key)]; dup {
			continue
		}
		seen[string(key)] = struct{}{}
		existing = append(existing, a)
	}
	return existing
}

func mergeByTag(existing, additions []any) []any {
	seenTags := map[string]struct{}{}
	seenContent := map[string]struct{}{}
	for _, e := range existing {
		if m, ok := e.(map[string]any); ok {
			if tag, ok := m["tag"].(string); ok && tag != "" {
				seenTags[tag] = struct{}{}
			}
		}
		if key, err := json.Marshal(e); err == nil {
			seenContent[string(key)] = struct{}{}
		}
	}
	for _, a := range additions {
		tag := ""
		if m, ok := a.(map[string]any); ok {
			tag, _ = m["tag"].(string)
		}
		if tag != "" {
			if _, dup := seenTags[tag]; dup {
				continue
			}
			seenTags[tag] = struct{}{}
		} else if key, err := json.Marshal(a); err == nil {
			if _, dup := seenContent[string(key)]; dup {
				continue
			}
			seenContent[string(key)] = struct{}{}
		}
		existing = append(existing, a)
	}
	return existing
}
