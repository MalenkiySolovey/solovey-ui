package formats

import (
	"fmt"
	"strings"

	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func xrayBalancers(groupOutbounds []map[string]interface{}, proxyTags []string) []map[string]interface{} {
	balancers := make([]map[string]interface{}, 0, len(groupOutbounds)+1)
	known := make(map[string]struct{}, len(proxyTags))
	for _, tag := range proxyTags {
		known[tag] = struct{}{}
	}
	for index, outbound := range groupOutbounds {
		balancer := xrayBalancer(outbound, known, index)
		if len(balancer) == 0 {
			continue
		}
		balancers = append(balancers, balancer)
	}
	if len(balancers) == 0 && len(proxyTags) > 0 {
		balancers = append(balancers, map[string]interface{}{
			"tag":      "proxy",
			"selector": proxyTags,
			"strategy": map[string]interface{}{"type": "leastPing"},
		})
	}
	return balancers
}
func xrayBalancer(outbound map[string]interface{}, known map[string]struct{}, index int) map[string]interface{} {
	tag := strings.TrimSpace(asString(outbound["tag"]))
	if tag == "" {
		tag = fmt.Sprintf("group-%d", index+1)
	}
	members := make([]string, 0)
	for _, member := range xrayStringList(outbound["outbounds"]) {
		if _, ok := known[member]; ok {
			members = appendUniqueXrayString(members, member)
		}
	}
	if len(members) == 0 {
		return nil
	}
	balancer := map[string]interface{}{
		"tag":      tag,
		"selector": members,
		"strategy": map[string]interface{}{"type": xrayBalancerStrategyForOutbound(outbound)},
	}
	if fallbackTag := asString(outbound["default"]); fallbackTag != "" {
		if _, ok := known[fallbackTag]; ok {
			balancer["fallbackTag"] = fallbackTag
		}
	}
	return balancer
}
func xrayBalancerStrategyForOutbound(outbound map[string]interface{}) string {
	for _, adaptation := range formatAdaptations(outbound) {
		if adaptation.SourceFormat == subcanonical.FormatXray &&
			adaptation.SourceFeature == "routing.balancer" &&
			adaptation.Strategy != "" {
			return adaptation.Strategy
		}
		if adaptation.SourceFormat != subcanonical.FormatClash || adaptation.SourceFeature != "proxy-groups" {
			continue
		}
		switch adaptation.SourceType {
		case "url-test", "fallback", "smart":
			return "leastPing"
		case "load-balance", "select", "relay", "ssid":
			return "random"
		}
	}
	return xrayBalancerStrategy(asString(outbound["type"]))
}
func xrayBalancerStrategy(outboundType string) string {
	switch outboundType {
	case "urltest":
		return "leastPing"
	default:
		return "random"
	}
}
