package formats

import (
	"fmt"
	"strings"
	"time"

	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func renderClashGroups(groupOutbounds []map[string]interface{}, proxyNameMap map[string]string, providerNames map[string]struct{}) ([]map[string]interface{}, []string) {
	if len(groupOutbounds) == 0 {
		return nil, nil
	}
	groupNameMap := uniqueClashGroupNameMap(groupOutbounds)
	refMap := make(map[string]string, len(proxyNameMap)+len(groupNameMap))
	for key, value := range proxyNameMap {
		refMap[key] = value
	}
	for key, value := range groupNameMap {
		refMap[key] = value
	}

	groups := make([]map[string]interface{}, 0, len(groupOutbounds))
	names := make([]string, 0, len(groupOutbounds))
	for index, outbound := range groupOutbounds {
		group := renderClashGroup(outbound, groupNameMap, refMap, providerNames, index)
		if group == nil {
			continue
		}
		groups = append(groups, group)
		names = append(names, asString(group["name"]))
	}
	return groups, names
}
func uniqueClashGroupNameMap(groupOutbounds []map[string]interface{}) map[string]string {
	seen := map[string]bool{
		"Proxy": true,
		"Auto":  true,
	}
	names := make(map[string]string, len(groupOutbounds))
	for index, outbound := range groupOutbounds {
		original := strings.TrimSpace(asString(outbound["tag"]))
		if original == "" {
			original = fmt.Sprintf("group-%d", index+1)
		}
		name := original
		for suffix := 2; seen[name]; suffix++ {
			name = fmt.Sprintf("%s-%d", original, suffix)
		}
		seen[name] = true
		names[original] = name
		names[name] = name
	}
	return names
}
func renderClashGroup(outbound map[string]interface{}, groupNameMap map[string]string, refMap map[string]string, providerNames map[string]struct{}, index int) map[string]interface{} {
	groupType := clashGroupType(outbound)
	if groupType == "" {
		return nil
	}
	name := strings.TrimSpace(asString(outbound["tag"]))
	if name == "" {
		name = fmt.Sprintf("group-%d", index+1)
	}
	if mapped := groupNameMap[name]; mapped != "" {
		name = mapped
	}
	proxies := clashGroupRefs(outbound["outbounds"], refMap)
	if len(proxies) == 0 {
		return nil
	}
	group := map[string]interface{}{
		"name":    name,
		"type":    groupType,
		"proxies": proxies,
	}
	if clashTimedGroup(groupType) {
		if url := strings.TrimSpace(asString(outbound["url"])); url != "" {
			group["url"] = url
		} else {
			group["url"] = "http://www.gstatic.com/generate_204"
		}
		if interval := clashGroupInterval(outbound["interval"]); interval != nil {
			group["interval"] = interval
		}
		if tolerance, ok := outbound["tolerance"]; ok {
			group["tolerance"] = tolerance
		}
	}
	if groupType == "load-balance" {
		if strategy := clashGroupStrategy(outbound); strategy != "" {
			group["strategy"] = strategy
		}
	}
	applyClashGroupMetadata(group, outbound, providerNames)
	return group
}
func applyClashGroupMetadata(group map[string]interface{}, outbound map[string]interface{}, providerNames map[string]struct{}) {
	metadata := clashMihomoGroupMetadata(outbound["mihomo_group"])
	if len(metadata) == 0 {
		return
	}
	for _, key := range []string{
		"url",
		"interval",
		"tolerance",
		"lazy",
		"empty-fallback",
		"timeout",
		"max-failed-times",
		"disable-udp",
		"interface-name",
		"routing-mark",
		"expected-status",
		"hidden",
		"icon",
		"strategy",
	} {
		if value, ok := metadata[key]; ok && !clashEmptyMetadataValue(value) {
			group[key] = value
		}
	}
	if providers := clashAvailableProviderRefs(metadata["use"], providerNames); len(providers) > 0 {
		group["use"] = providers
	}
}
func clashMihomoGroupMetadata(value interface{}) map[string]interface{} {
	metadata, _ := value.(map[string]interface{})
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}
func clashProviderNames(value interface{}) map[string]struct{} {
	providers, _ := value.(map[string]interface{})
	if len(providers) == 0 {
		return nil
	}
	names := make(map[string]struct{}, len(providers))
	for name := range providers {
		name = strings.TrimSpace(name)
		if name != "" {
			names[name] = struct{}{}
		}
	}
	return names
}
func clashAvailableProviderRefs(value interface{}, providerNames map[string]struct{}) []string {
	if len(providerNames) == 0 {
		return nil
	}
	rawRefs := clashStringList(value)
	refs := make([]string, 0, len(rawRefs))
	seen := make(map[string]struct{}, len(rawRefs))
	for _, ref := range rawRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if _, ok := providerNames[ref]; !ok {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	return refs
}
func clashEmptyMetadataValue(value interface{}) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	if list, ok := value.([]interface{}); ok {
		return len(list) == 0
	}
	if list, ok := value.([]string); ok {
		return len(list) == 0
	}
	return false
}
func clashGroupType(outbound map[string]interface{}) string {
	if targetType := clashTargetGroupType(outbound); targetType != "" {
		return targetType
	}
	if sourceType := clashSourceGroupType(outbound); sourceType != "" {
		return sourceType
	}
	switch strings.TrimSpace(asString(outbound["type"])) {
	case "selector":
		return "select"
	case "urltest":
		return "url-test"
	case "failover":
		return "fallback"
	default:
		return ""
	}
}
func clashTargetGroupType(outbound map[string]interface{}) string {
	for _, adaptation := range formatAdaptations(outbound) {
		switch adaptation.TargetType {
		case "select", "url-test", "fallback", "load-balance", "relay", "smart", "ssid":
			return adaptation.TargetType
		}
	}
	return ""
}
func clashSourceGroupType(outbound map[string]interface{}) string {
	for _, adaptation := range formatAdaptations(outbound) {
		if adaptation.SourceFormat != subcanonical.FormatClash || adaptation.SourceFeature != "proxy-groups" {
			continue
		}
		switch adaptation.SourceType {
		case "select", "url-test", "fallback", "load-balance", "relay", "smart", "ssid":
			return adaptation.SourceType
		}
	}
	return ""
}
func clashGroupStrategy(outbound map[string]interface{}) string {
	targetLoadBalance := false
	for _, adaptation := range formatAdaptations(outbound) {
		if adaptation.SourceFormat == subcanonical.FormatClash &&
			adaptation.SourceFeature == "proxy-groups" &&
			adaptation.SourceType == "load-balance" {
			return strings.TrimSpace(adaptation.Strategy)
		}
		if adaptation.TargetType == "load-balance" {
			targetLoadBalance = true
		}
	}
	if targetLoadBalance {
		for _, adaptation := range formatAdaptations(outbound) {
			if adaptation.SourceFormat == subcanonical.FormatXray &&
				adaptation.SourceFeature == "routing.balancer" &&
				strings.EqualFold(adaptation.Strategy, "random") {
				return "round-robin"
			}
		}
	}
	return ""
}
func clashTimedGroup(groupType string) bool {
	switch groupType {
	case "url-test", "fallback", "load-balance", "smart":
		return true
	default:
		return false
	}
}
func clashGroupRefs(value interface{}, refMap map[string]string) []string {
	rawRefs := clashStringList(value)
	refs := make([]string, 0, len(rawRefs))
	for _, ref := range rawRefs {
		mapped := refMap[ref]
		if mapped == "" {
			continue
		}
		exists := false
		for _, existing := range refs {
			if existing == mapped {
				exists = true
				break
			}
		}
		if !exists {
			refs = append(refs, mapped)
		}
	}
	return refs
}
func clashStringList(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := strings.TrimSpace(asString(item)); value != "" {
				result = append(result, value)
			}
		}
		return result
	default:
		return nil
	}
}
func clashGroupInterval(value interface{}) interface{} {
	switch typed := value.(type) {
	case int, int64, uint, float64:
		return typed
	case string:
		trimmed := strings.TrimSpace(typed)
		if duration, err := time.ParseDuration(trimmed); err == nil {
			return int(duration.Seconds())
		}
		if trimmed != "" {
			return trimmed
		}
	}
	return nil
}
