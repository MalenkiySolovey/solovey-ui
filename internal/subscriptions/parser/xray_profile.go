package parser

import (
	"fmt"
	"strings"

	subconversion "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/conversion"
)

const xrayBalancerProbeURL = "http://www.gstatic.com/generate_204"

func xrayConfigHasBalancers(config map[string]interface{}) bool {
	routing, _ := config["routing"].(map[string]interface{})
	return len(xrayList(routing["balancers"])) > 0
}
func xrayConfigProfileOutbound(config map[string]interface{}, members []map[string]interface{}, aliases []xrayTagAliases, options ParseOptions, configIndex int, multiConfig bool) map[string]interface{} {
	if len(members) == 0 {
		return nil
	}

	profile := cloneXrayMap(members[0])
	if primaryTag := strings.TrimSpace(stringValue(profile["tag"])); primaryTag != "" {
		profile["xray_primary_tag"] = primaryTag
	}
	profile["tag"] = xrayProfileTag(config, profile, configIndex, multiConfig)
	profile["xray_profile"] = true
	profile["xray_profile_outbounds"] = members

	profileType := "custom"
	if len(aliases) > 0 {
		balancers := xrayProfileBalancers(config, aliases, options)
		if len(balancers) > 0 {
			profile["xray_profile_balancers"] = balancers
		}
	}
	if _, ok := profile["xray_profile_balancers"]; ok {
		profileType = "balancer"
	}
	profile["xray_profile_type"] = profileType
	markXrayProfileAdaptation(profile, profileType, strings.TrimSpace(stringValue(profile["type"])))
	return profile
}
func xrayProfileMemberOutbounds(rawOutbounds []interface{}) ([]map[string]interface{}, []xrayTagAliases) {
	members := make([]map[string]interface{}, 0, len(rawOutbounds))
	aliases := make([]xrayTagAliases, 0, len(rawOutbounds))
	for index, raw := range rawOutbounds {
		xrayOutbound, ok := raw.(map[string]interface{})
		if !ok || len(xrayOutbound) == 0 {
			continue
		}
		protocol := strings.ToLower(strings.TrimSpace(stringValue(xrayOutbound["protocol"])))
		if protocol == "" {
			continue
		}
		nodes := xrayProtocolOutbounds(protocol, xrayOutbound, index)
		if len(nodes) == 0 {
			continue
		}
		baseTag := xrayOutboundBaseTag(xrayOutbound, index)
		generated := make([]string, 0, len(nodes))
		for nodeIndex := range nodes {
			sourceTag := xrayVariantTag(baseTag, nodeIndex, len(nodes))
			nodes[nodeIndex]["tag"] = sourceTag
			nodes[nodeIndex]["xray_tag"] = sourceTag
			generated = append(generated, sourceTag)
		}
		members = append(members, nodes...)
		aliases = append(aliases, xrayTagAliases{
			Original:  baseTag,
			Generated: generated,
		})
	}
	return members, aliases
}
func xrayScopedProfileMembers(scope string, members []map[string]interface{}, aliases []xrayTagAliases) ([]map[string]interface{}, []xrayTagAliases) {
	scope = strings.TrimSpace(scope)
	scopedTags := make(map[string]string, len(members))
	scopedMembers := make([]map[string]interface{}, 0, len(members))
	for _, member := range members {
		scoped := cloneXrayMap(member)
		originalTag := strings.TrimSpace(stringValue(scoped["xray_tag"]))
		if originalTag == "" {
			originalTag = strings.TrimSpace(stringValue(scoped["tag"]))
		}
		if originalTag != "" {
			scoped["xray_tag"] = originalTag
			scoped["tag"] = xrayScopedTag(scope, originalTag)
			scopedTags[originalTag] = stringValue(scoped["tag"])
		}
		scoped["xray_profile_member"] = true
		if scope != "" {
			scoped["xray_profile_owner"] = scope
		}
		scopedMembers = append(scopedMembers, scoped)
	}
	scopedAliases := make([]xrayTagAliases, 0, len(aliases))
	for _, alias := range aliases {
		scoped := xrayTagAliases{
			Original:  alias.Original,
			Generated: make([]string, 0, len(alias.Generated)),
		}
		for _, generated := range alias.Generated {
			if scopedTag := scopedTags[generated]; scopedTag != "" {
				scoped.Generated = append(scoped.Generated, scopedTag)
				continue
			}
			scoped.Generated = append(scoped.Generated, xrayScopedTag(scope, generated))
		}
		scopedAliases = append(scopedAliases, scoped)
	}
	return scopedMembers, scopedAliases
}
func xrayProfileTag(config map[string]interface{}, profile map[string]interface{}, configIndex int, multiConfig bool) string {
	if remarks := strings.TrimSpace(stringValue(config["remarks"])); remarks != "" {
		return remarks
	}
	if tag := strings.TrimSpace(stringValue(profile["tag"])); tag != "" {
		return tag
	}
	if multiConfig {
		return fmt.Sprintf("xray-config-%d", configIndex+1)
	}
	return "xray-config"
}
func xrayProfileBalancers(config map[string]interface{}, aliases []xrayTagAliases, options ParseOptions) []map[string]interface{} {
	routing, _ := config["routing"].(map[string]interface{})
	rawBalancers := xrayList(routing["balancers"])
	if len(rawBalancers) == 0 {
		return nil
	}
	targetType := conversionTarget(options, subconversion.FeatureForXrayBalancer())
	balancers := make([]map[string]interface{}, 0, len(rawBalancers))
	for index, raw := range rawBalancers {
		balancer, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		entry := xrayProfileBalancerEntry(balancer, index, aliases, targetType)
		balancers = append(balancers, entry)
	}
	return balancers
}
func xrayProfileBalancerEntry(balancer map[string]interface{}, index int, aliases []xrayTagAliases, targetType string) map[string]interface{} {
	sourceTag := xrayBalancerSourceTag(balancer, index)
	selectors := xrayStringList(balancer["selector"])
	members := xrayBalancerMembers(selectors, aliases)
	entry := map[string]interface{}{
		"type":        "balancer",
		"tag":         sourceTag,
		"target_type": targetType,
		"selector":    selectors,
		"members":     members,
	}
	if fallback := strings.TrimSpace(stringValue(balancer["fallbackTag"])); fallback != "" {
		entry["fallback_tag"] = fallback
		if fallbackMembers := xrayBalancerMembers([]string{fallback}, aliases); len(fallbackMembers) > 0 {
			entry["fallback_member"] = fallbackMembers[0]
		}
	}
	if strategy := xrayBalancerStrategy(balancer); strategy != "" {
		entry["strategy"] = strategy
	}
	return entry
}
func xrayBalancerOutbounds(config map[string]interface{}, aliases []xrayTagAliases, profileMembers []map[string]interface{}, options ParseOptions, configIndex int, multiConfig bool) []map[string]interface{} {
	routing, _ := config["routing"].(map[string]interface{})
	rawBalancers := xrayList(routing["balancers"])
	if len(rawBalancers) == 0 {
		return nil
	}
	groups := make([]map[string]interface{}, 0, len(rawBalancers))
	for index, raw := range rawBalancers {
		balancer, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		sourceTag := xrayBalancerSourceTag(balancer, index)
		tag := xrayBalancerDisplayTag(config, sourceTag, len(rawBalancers), configIndex, multiConfig)
		memberRefs := xrayBalancerMembers(xrayStringList(balancer["selector"]), aliases)
		var fallbackMembers []string
		if fallback := stringValue(balancer["fallbackTag"]); fallback != "" {
			fallbackMembers = xrayBalancerMembers([]string{fallback}, aliases)
			memberRefs = appendUniqueStrings(memberRefs, fallbackMembers...)
		}
		if len(memberRefs) == 0 {
			continue
		}
		targetType := conversionTarget(options, subconversion.FeatureForXrayBalancer())
		group := xrayAdaptedGroupOutbound(targetType, tag, memberRefs)
		if len(fallbackMembers) > 0 && targetType != GroupAdaptationURLTest {
			group["default"] = fallbackMembers[0]
		}
		if tag != sourceTag {
			group["xray_tag"] = sourceTag
		}
		group["xray_profile"] = true
		group["xray_profile_type"] = "balancer"
		group["xray_profile_outbounds"] = cloneXrayMaps(profileMembers)
		group["xray_profile_balancers"] = []map[string]interface{}{
			xrayProfileBalancerEntry(balancer, index, aliases, targetType),
		}
		markXrayBalancerAdaptation(group, targetType, xrayBalancerStrategy(balancer))
		groups = append(groups, group)
	}
	return groups
}
func xrayBalancerProfileOutbound(config map[string]interface{}, groups []map[string]interface{}, aliases []xrayTagAliases, options ParseOptions, configIndex int, multiConfig bool) map[string]interface{} {
	if len(groups) == 0 {
		return nil
	}
	profile := cloneXrayMap(groups[0])
	profile["tag"] = xrayProfileTag(config, profile, configIndex, multiConfig)
	profile["xray_profile_balancers"] = xrayProfileBalancers(config, aliases, options)
	return profile
}
func xrayBalancerSourceTag(balancer map[string]interface{}, index int) string {
	if tag := strings.TrimSpace(stringValue(balancer["tag"])); tag != "" {
		return tag
	}
	return fmt.Sprintf("xray-balancer-%d", index+1)
}
func xrayBalancerDisplayTag(config map[string]interface{}, sourceTag string, balancerCount int, configIndex int, multiConfig bool) string {
	remarks := strings.TrimSpace(stringValue(config["remarks"]))
	if remarks == "" {
		if multiConfig {
			return xrayScopedTag(fmt.Sprintf("xray-config-%d", configIndex+1), sourceTag)
		}
		return sourceTag
	}
	if balancerCount <= 1 {
		return remarks
	}
	return xrayScopedTag(remarks, sourceTag)
}
func xrayAdaptedGroupOutbound(targetType string, tag string, members []string) map[string]interface{} {
	switch targetType {
	case GroupAdaptationSelector:
		return map[string]interface{}{
			"type":      "selector",
			"tag":       tag,
			"outbounds": members,
			"default":   members[0],
		}
	case GroupAdaptationFailover:
		return map[string]interface{}{
			"type":      "failover",
			"tag":       tag,
			"outbounds": members,
			"default":   members[0],
			"failover": map[string]interface{}{
				"enabled":      true,
				"probe_target": xrayBalancerProbeURL,
				"interval":     "10m",
				"hysteresis":   2,
			},
		}
	default:
		return map[string]interface{}{
			"type":      "urltest",
			"tag":       tag,
			"outbounds": members,
			"url":       xrayBalancerProbeURL,
			"interval":  "10m",
			"tolerance": 50,
		}
	}
}
func xrayBalancerStrategy(balancer map[string]interface{}) string {
	strategy, _ := balancer["strategy"].(map[string]interface{})
	return strings.TrimSpace(stringValue(strategy["type"]))
}
func xrayBalancerMembers(selectors []string, aliases []xrayTagAliases) []string {
	members := make([]string, 0)
	for _, selector := range selectors {
		selector = strings.TrimSpace(selector)
		if selector == "" {
			continue
		}
		for _, alias := range aliases {
			if strings.HasPrefix(alias.Original, selector) {
				members = appendUniqueStrings(members, alias.Generated...)
				continue
			}
			for _, generated := range alias.Generated {
				if strings.HasPrefix(generated, selector) {
					members = appendUniqueStrings(members, generated)
				}
			}
		}
	}
	return members
}
