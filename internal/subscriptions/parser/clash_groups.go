package parser

import (
	"fmt"
	"strings"

	subconversion "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/conversion"
)

func clashProxyGroupOutbounds(config map[string]interface{}, knownTags map[string]struct{}, options ParseOptions) []map[string]interface{} {
	rawGroups := xrayList(config["proxy-groups"])
	if len(rawGroups) == 0 {
		return nil
	}
	outbounds := make([]map[string]interface{}, 0, len(rawGroups))
	for index, raw := range rawGroups {
		group, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		outbound := clashProxyGroupOutbound(group, knownTags, options, index)
		if outbound == nil {
			continue
		}
		outbounds = append(outbounds, outbound)
	}
	return outbounds
}
func clashProxyGroupOutbound(group map[string]interface{}, knownTags map[string]struct{}, options ParseOptions, index int) map[string]interface{} {
	groupType := strings.ToLower(strings.TrimSpace(stringValue(group["type"])))
	outboundType, note := clashProxyGroupTarget(groupType, options)
	if outboundType == "" {
		return nil
	}
	members := clashProxyGroupMembers(group["proxies"], knownTags)
	if len(members) == 0 {
		return nil
	}
	tag := strings.TrimSpace(stringValue(group["name"]))
	if tag == "" {
		tag = fmt.Sprintf("clash-group-%d", index+1)
	}
	outbound := map[string]interface{}{
		"type":      outboundType,
		"tag":       tag,
		"outbounds": members,
	}
	if metadata := clashProxyGroupMetadata(group, groupType, members); len(metadata) > 0 {
		outbound["mihomo_group"] = metadata
	}
	markClashGroupAdaptation(outbound, groupType, outboundType, firstString(group["strategy"]), note)
	if groupType == "select" {
		outbound["_subscription_auxiliary"] = true
	}
	switch outboundType {
	case "selector":
		outbound["default"] = members[0]
	case "urltest":
		outbound["url"] = firstString(group["url"])
		if outbound["url"] == "" {
			outbound["url"] = xrayBalancerProbeURL
		}
		outbound["interval"] = clashProxyGroupInterval(group["interval"])
		if tolerance, ok := group["tolerance"]; ok {
			outbound["tolerance"] = tolerance
		} else {
			outbound["tolerance"] = 50
		}
	case "failover":
		outbound["default"] = members[0]
		probeTarget := firstString(group["url"])
		if probeTarget == "" {
			probeTarget = xrayBalancerProbeURL
		}
		outbound["failover"] = map[string]interface{}{
			"enabled":      true,
			"probe_target": probeTarget,
			"interval":     clashProxyGroupInterval(group["interval"]),
			"hysteresis":   2,
		}
	}
	return outbound
}
func clashProxyGroupMetadata(group map[string]interface{}, groupType string, members []string) map[string]interface{} {
	metadata := map[string]interface{}{
		"type":    groupType,
		"proxies": append([]string(nil), members...),
	}
	for _, key := range []string{
		"name",
		"use",
		"url",
		"interval",
		"lazy",
		"empty-fallback",
		"timeout",
		"max-failed-times",
		"disable-udp",
		"interface-name",
		"routing-mark",
		"include-all",
		"include-all-proxies",
		"include-all-providers",
		"filter",
		"exclude-filter",
		"exclude-type",
		"expected-status",
		"hidden",
		"icon",
		"strategy",
	} {
		if value, ok := group[key]; ok {
			metadata[key] = value
		}
	}
	return metadata
}
func clashProxyGroupTarget(groupType string, options ParseOptions) (string, string) {
	switch groupType {
	case "select":
		return "selector", ""
	case "url-test":
		return "urltest", ""
	case "fallback":
		return conversionTarget(options, subconversion.FeatureForMihomoGroup(groupType)), "fallback order is adapted for subscription runtime"
	case "load-balance":
		return conversionTarget(options, subconversion.FeatureForMihomoGroup(groupType)), "load-balance distribution is adapted for subscription runtime"
	case "smart":
		return conversionTarget(options, subconversion.FeatureForMihomoGroup(groupType)), "mihomo smart group is adapted for subscription runtime"
	case "relay":
		return conversionTarget(options, subconversion.FeatureForMihomoGroup(groupType)), "relay chaining is adapted to sing-box selector for subscription runtime"
	case "ssid":
		return conversionTarget(options, subconversion.FeatureForMihomoGroup(groupType)), "ssid policy is adapted to sing-box selector for subscription runtime"
	default:
		return "", ""
	}
}
func clashProxyGroupMembers(value interface{}, knownTags map[string]struct{}) []string {
	rawMembers := xrayStringList(value)
	members := make([]string, 0, len(rawMembers))
	for _, member := range rawMembers {
		if _, ok := knownTags[member]; !ok {
			continue
		}
		members = appendUniqueStrings(members, member)
	}
	return members
}
func clashProxyGroupInterval(value interface{}) string {
	switch typed := value.(type) {
	case int:
		return fmt.Sprintf("%ds", typed)
	case int64:
		return fmt.Sprintf("%ds", typed)
	case uint:
		return fmt.Sprintf("%ds", typed)
	case float64:
		if typed > 0 {
			return fmt.Sprintf("%.0fs", typed)
		}
	case string:
		if strings.TrimSpace(typed) != "" {
			return strings.TrimSpace(typed)
		}
	}
	return "10m"
}
