package importxui

import (
	"encoding/json"
	"fmt"
	"strings"
)

// resolveRoutingTarget maps an Xray outboundTag to an s-ui routing target.
func resolveRoutingTarget(outboundTag string, targets map[string]string) (string, bool) {
	if t, ok := targets[outboundTag]; ok && t != "" {
		return t, true
	}
	switch strings.ToLower(outboundTag) {
	case "block", "blocked":
		return rejectTarget, true
	case "direct":
		return directOutboundTag, true
	}
	return "", false
}

func MapXrayRouting(raw string, targets map[string]string) (map[string]any, []string, int, int) {
	result := map[string]any{
		"route": map[string]any{
			"rules":    []any{},
			"rule_set": []any{},
		},
		"dns": map[string]any{},
	}
	if strings.TrimSpace(raw) == "" {
		return result, nil, 0, 0
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return result, []string{fmt.Sprintf("routing: invalid xrayConfig: %v", err)}, 0, 1
	}
	route := result["route"].(map[string]any)
	rulesOut := route["rules"].([]any)
	ruleSets := route["rule_set"].([]any)
	seenRuleSet := map[string]struct{}{}
	mapped := 0
	manual := 0
	var warnings []string
	routing, _ := cfg["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	for index, rawRule := range rules {
		rule, _ := rawRule.(map[string]any)
		if rule == nil {
			continue
		}
		if _, ok := rule["balancerTag"]; ok {
			manual++
			warnings = append(warnings, fmt.Sprintf("routing rule %d uses balancer; manual review required", index))
			continue
		}
		if _, ok := rule["attrs"]; ok {
			manual++
			warnings = append(warnings, fmt.Sprintf("routing rule %d uses attrs (HTTP attribute match) which sing-box does not support; manual review required", index))
			continue
		}
		outboundTag, _ := rule["outboundTag"].(string)
		outboundTag = strings.TrimSpace(outboundTag)
		if outboundTag == "" {
			manual++
			warnings = append(warnings, fmt.Sprintf("routing rule %d has no outboundTag; manual review required", index))
			continue
		}
		target, ok := resolveRoutingTarget(outboundTag, targets)
		if !ok {
			manual++
			warnings = append(warnings, fmt.Sprintf("routing rule %d outbound %q requires manual review", index, outboundTag))
			continue
		}
		next := map[string]any{}
		switch target {
		case dnsHijackTarget:
			next["action"] = "hijack-dns"
		case rejectTarget:
			next["action"] = "reject"
		default:
			next["outbound"] = target
		}
		matched, matcherWarnings := applyRuleMatchers(index, rule, next, &ruleSets, seenRuleSet)
		warnings = append(warnings, matcherWarnings...)
		if !matched {
			manual++
			warnings = append(warnings, fmt.Sprintf("routing rule %d has no supported matchers; manual review required", index))
			continue
		}
		rulesOut = append(rulesOut, next)
		mapped++
	}
	if dns, ok := cfg["dns"].(map[string]any); ok {
		dnsOut, dnsWarnings := mapXrayDNS(dns, &ruleSets, seenRuleSet)
		warnings = append(warnings, dnsWarnings...)
		result["dns"] = dnsOut
	}
	route["rules"] = rulesOut
	route["rule_set"] = ruleSets
	return result, warnings, mapped, manual
}
