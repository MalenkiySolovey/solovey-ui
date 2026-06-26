package remote

import (
	"encoding/json"
	"strings"

	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	subconversion "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/conversion"
)

func canonicalAdaptations(raw json.RawMessage) []subcanonical.Adaptation {
	connection := canonicalConnection(raw)
	if connection == nil {
		return nil
	}
	return connection.Adaptations
}
func canonicalConnection(raw json.RawMessage) *subcanonical.Connection {
	if len(raw) == 0 {
		return nil
	}
	var connection subcanonical.Connection
	if err := json.Unmarshal(raw, &connection); err != nil {
		return nil
	}
	return &connection
}
func attachAdaptations(outbound map[string]interface{}, adaptations []subcanonical.Adaptation) {
	if len(adaptations) == 0 || outbound == nil {
		return
	}
	outbound[subcanonical.MetadataKey] = adaptations
}
func attachNativeClientMetadata(outbound map[string]interface{}, canonical json.RawMessage, target string) {
	if outbound == nil || strings.TrimSpace(target) != subconversion.TargetMihomo {
		return
	}
	metadata := canonicalMihomoGroupMetadata(canonical)
	if len(metadata) == 0 {
		return
	}
	outbound["mihomo_group"] = metadata
}
func canonicalMihomoGroupMetadata(raw json.RawMessage) map[string]interface{} {
	connection := canonicalConnection(raw)
	if connection == nil {
		return nil
	}
	if metadata := outboundMapValue(connection.BestOutbound, "mihomo_group"); len(metadata) > 0 {
		return metadata
	}
	for _, observation := range connection.Observations {
		if observation.Format != subcanonical.FormatClash {
			continue
		}
		if metadata := outboundMapValue(observation.Outbound, "mihomo_group"); len(metadata) > 0 {
			return metadata
		}
	}
	return nil
}
func outboundMapValue(outbound map[string]any, key string) map[string]interface{} {
	value, ok := outbound[key]
	if !ok {
		return nil
	}
	typed, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	return cloneOutboundMap(typed)
}
func attachClientConversionAdaptations(outbound map[string]interface{}, adaptations []subcanonical.Adaptation, target string, mode string) {
	if outbound == nil {
		return
	}
	if target != subconversion.TargetMihomo && target != subconversion.TargetXray {
		return
	}
	result := make([]subcanonical.Adaptation, 0, len(adaptations)+1)
	result = append(result, adaptations...)
	if target == subconversion.TargetMihomo {
		result = append(result, subcanonical.Adaptation{
			SourceFeature: "client.conversion",
			SourceType:    target,
			TargetType:    mode,
		})
	}
	if target == subconversion.TargetXray {
		result = append(result, subcanonical.Adaptation{
			SourceFeature: "client.conversion",
			SourceType:    target,
			TargetType:    mode,
		})
	}
	attachAdaptations(outbound, result)
}
func conversionFeature(adaptations []subcanonical.Adaptation) string {
	for _, adaptation := range adaptations {
		if adaptation.SourceFormat == subcanonical.FormatXray && adaptation.SourceFeature == "routing.balancer" {
			return subconversion.FeatureForXrayBalancer()
		}
		if adaptation.SourceFormat == subcanonical.FormatClash && adaptation.SourceFeature == "proxy-groups" {
			if feature := subconversion.FeatureForMihomoGroup(adaptation.SourceType); feature != "" {
				return feature
			}
		}
	}
	return ""
}
