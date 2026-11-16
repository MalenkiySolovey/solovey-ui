//go:build !minimal

package remote

import (
	subcanonical "github.com/MalenkiySolovey/solovey-host/subscriptions/canonical"
	"strings"
)

func RewriteOutboundTagReferences(raw map[string]any, tagMap map[string]string) {
	if len(tagMap) == 0 || raw == nil {
		return
	}
	if value, ok := raw["outbounds"]; ok {
		raw["outbounds"] = RewriteOutboundTagList(value, tagMap)
	}
	if route, ok := raw["route"].(map[string]any); ok {
		if tag, ok := route["final"].(string); ok {
			route["final"] = RewriteOutboundTag(tag, tagMap)
		}
		var rewriteRules func([]any)
		rewriteRules = func(rules []any) {
			for _, item := range rules {
				if rule, ok := item.(map[string]any); ok {
					if tag, ok := rule["outbound"].(string); ok {
						rule["outbound"] = RewriteOutboundTag(tag, tagMap)
					}
					if children, ok := rule["rules"].([]any); ok {
						rewriteRules(children)
					}
				}
			}
		}
		if rules, ok := route["rules"].([]any); ok {
			rewriteRules(rules)
		}
		if sets, ok := route["rule_set"].([]any); ok {
			for _, item := range sets {
				if set, ok := item.(map[string]any); ok {
					if tag, ok := set["download_detour"].(string); ok {
						set["download_detour"] = RewriteOutboundTag(tag, tagMap)
					}
				}
			}
		}
	}
	if value, ok := raw["default"].(string); ok {
		if rewritten := tagMap[strings.TrimSpace(value)]; rewritten != "" {
			raw["default"] = rewritten
		}
	}
}
func RewriteOutboundTagList(value any, tagMap map[string]string) any {
	switch typed := value.(type) {
	case []string:
		rewritten := make([]string, 0, len(typed))
		for _, tag := range typed {
			rewritten = append(rewritten, RewriteOutboundTag(tag, tagMap))
		}
		return rewritten
	case []any:
		rewritten := make([]any, 0, len(typed))
		for _, item := range typed {
			switch value := item.(type) {
			case string:
				rewritten = append(rewritten, RewriteOutboundTag(value, tagMap))
			case map[string]any:
				leg := subcanonical.CloneOutbound(value)
				if tag, ok := leg["outbound"].(string); ok {
					leg["outbound"] = RewriteOutboundTag(tag, tagMap)
				}
				rewritten = append(rewritten, leg)
			default:
				// Preserve unknown carriers for the canonical owner's rejection.
				rewritten = append(rewritten, item)
			}
		}
		return rewritten
	default:
		return value
	}
}
func RewriteOutboundTag(tag string, tagMap map[string]string) string {
	trimmed := strings.TrimSpace(tag)
	if rewritten := tagMap[trimmed]; rewritten != "" {
		return rewritten
	}
	return tag
}
