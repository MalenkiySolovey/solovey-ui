package parser

import subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"

func markXrayBalancerAdaptation(outbound map[string]interface{}, targetType string, strategy string) {
	markSubscriptionAdaptation(outbound, subcanonical.FormatXray, "routing.balancer", "balancer", targetType, strategy, "")
}

func markXrayProfileAdaptation(outbound map[string]interface{}, sourceType string, targetType string) {
	markSubscriptionAdaptation(outbound, subcanonical.FormatXray, "custom.config", sourceType, targetType, "", "")
}

func markXrayTransportAdaptation(outbound map[string]interface{}, sourceType string, targetType string, note string) {
	markSubscriptionAdaptation(outbound, subcanonical.FormatXray, "streamSettings", sourceType, targetType, "", note)
}

func markClashGroupAdaptation(outbound map[string]interface{}, sourceType string, targetType string, strategy string, note string) {
	markSubscriptionAdaptation(outbound, subcanonical.FormatClash, "proxy-groups", sourceType, targetType, strategy, note)
}

func markSubscriptionAdaptation(outbound map[string]interface{}, sourceFormat string, sourceFeature string, sourceType string, targetType string, strategy string, note string) {
	if outbound == nil {
		return
	}
	outbound[subcanonical.MetadataKey] = map[string]interface{}{
		"source_format":  sourceFormat,
		"source_feature": sourceFeature,
		"source_type":    sourceType,
		"target_type":    targetType,
		"strategy":       strategy,
		"note":           note,
	}
}
