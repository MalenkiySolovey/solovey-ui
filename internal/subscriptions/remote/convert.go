package remote

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	subconversion "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/conversion"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

type ClientLink struct {
	Type                 string `json:"type"`
	GroupId              uint   `json:"groupId"`
	RemoteGroupId        uint   `json:"remoteGroupId"`
	SubscriptionId       uint   `json:"subscriptionId"`
	RemoteSubscriptionId uint   `json:"remoteSubscriptionId"`
}
type clientLinkSelection struct {
	Kind string
	ID   uint
}
type ClientConversionOptions struct {
	Target string
	Policy subconversion.Policy
}

func ConnectionOutboundConfig(connection model.RemoteOutboundConnection) (json.RawMessage, error) {
	return connectionOutboundConfig(connection, nil)
}
func connectionOutboundConfig(connection model.RemoteOutboundConnection, tagMap map[string]string) (json.RawMessage, error) {
	if strings.TrimSpace(connection.Type) == "" {
		return nil, common.NewError("connection outbound type is empty")
	}
	if strings.TrimSpace(connection.OutboundTag) == "" {
		return nil, common.NewError("connection outbound tag is empty")
	}
	raw := map[string]any{}
	if len(connection.Options) > 0 {
		if err := json.Unmarshal(connection.Options, &raw); err != nil {
			return nil, err
		}
	}
	raw = subcanonical.CleanOutbound(raw)
	raw["type"] = connection.Type
	raw["tag"] = connection.OutboundTag
	rewriteOutboundTagReferences(raw, tagMap)
	normalizeRuntimeOutbound(raw)
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	return data, nil
}
func ConnectionOutboundMap(connection model.RemoteOutboundConnection) (map[string]interface{}, error) {
	return connectionOutboundMap(connection, nil)
}
func connectionOutboundMap(connection model.RemoteOutboundConnection, tagMap map[string]string) (map[string]interface{}, error) {
	rawConfig, err := connectionOutboundConfig(connection, tagMap)
	if err != nil {
		return nil, err
	}
	outbound := map[string]interface{}{}
	if err := json.Unmarshal(rawConfig, &outbound); err != nil {
		return nil, err
	}
	return outbound, nil
}
func connectionOutboundMapForClient(connection model.RemoteOutboundConnection, tagMap map[string]string, options ClientConversionOptions) (map[string]interface{}, error) {
	outbound, err := connectionOutboundMap(connection, tagMap)
	if err != nil {
		return nil, err
	}
	return applyClientConversion(connection, outbound, options), nil
}
func applyClientConversion(connection model.RemoteOutboundConnection, outbound map[string]interface{}, options ClientConversionOptions) map[string]interface{} {
	if outbound == nil {
		return nil
	}
	adaptations := canonicalAdaptations(connection.Canonical)
	feature := conversionFeature(adaptations)
	if feature == "" {
		attachAdaptations(outbound, adaptations)
		attachNativeClientMetadata(outbound, connection.Canonical, options.Target)
		return outbound
	}
	target := strings.TrimSpace(options.Target)
	if target == "" {
		target = subconversion.TargetSingBox
	}
	mode := options.Policy.Mode(target, feature)
	if mode == subconversion.ModeOriginal {
		attachAdaptations(outbound, adaptations)
		attachNativeClientMetadata(outbound, connection.Canonical, target)
		return outbound
	}
	outbound = convertClientGroupOutbound(outbound, target, mode)
	attachClientConversionAdaptations(outbound, adaptations, target, mode)
	attachNativeClientMetadata(outbound, connection.Canonical, target)
	return outbound
}
func convertGroupOutbound(outbound map[string]interface{}, mode string) map[string]interface{} {
	return convertRuntimeGroupOutbound(outbound, mode)
}
func convertClientGroupOutbound(outbound map[string]interface{}, target string, mode string) map[string]interface{} {
	switch strings.TrimSpace(target) {
	case subconversion.TargetXray:
		return xrayClientGroupOutbound(outbound, mode)
	case subconversion.TargetMihomo:
		return mihomoClientGroupOutbound(outbound, mode)
	default:
		return convertRuntimeGroupOutbound(outbound, mode)
	}
}
func convertRuntimeGroupOutbound(outbound map[string]interface{}, mode string) map[string]interface{} {
	switch mode {
	case subconversion.ModeSelector:
		return selectorGroupOutbound(outbound)
	case subconversion.ModeFailover:
		return failoverGroupOutbound(outbound)
	default:
		return urlTestGroupOutbound(outbound)
	}
}
func xrayClientGroupOutbound(outbound map[string]interface{}, mode string) map[string]interface{} {
	switch mode {
	case subconversion.ModeXrayBalancer:
		return selectorGroupOutbound(outbound)
	default:
		return selectorGroupOutbound(outbound)
	}
}
func mihomoClientGroupOutbound(outbound map[string]interface{}, mode string) map[string]interface{} {
	switch mode {
	case subconversion.ModeMihomoSelect:
		return selectorGroupOutbound(outbound)
	case subconversion.ModeMihomoFallback:
		return failoverGroupOutbound(outbound)
	case subconversion.ModeMihomoLoadBalance:
		return urlTestGroupOutbound(outbound)
	default:
		return urlTestGroupOutbound(outbound)
	}
}
func selectorGroupOutbound(outbound map[string]interface{}) map[string]interface{} {
	outbound["type"] = "selector"
	delete(outbound, "url")
	delete(outbound, "interval")
	delete(outbound, "tolerance")
	delete(outbound, "failover")
	members := stringList(outbound["outbounds"])
	if len(members) > 0 && strings.TrimSpace(fmt.Sprint(outbound["default"])) == "" {
		outbound["default"] = members[0]
	}
	return outbound
}
func urlTestGroupOutbound(outbound map[string]interface{}) map[string]interface{} {
	outbound["type"] = "urltest"
	delete(outbound, "default")
	delete(outbound, "failover")
	if strings.TrimSpace(fmt.Sprint(outbound["url"])) == "" || fmt.Sprint(outbound["url"]) == "<nil>" {
		outbound["url"] = "http://www.gstatic.com/generate_204"
	}
	if strings.TrimSpace(fmt.Sprint(outbound["interval"])) == "" || fmt.Sprint(outbound["interval"]) == "<nil>" {
		outbound["interval"] = "10m"
	}
	if _, ok := outbound["tolerance"]; !ok {
		outbound["tolerance"] = 50
	}
	return outbound
}
func failoverGroupOutbound(outbound map[string]interface{}) map[string]interface{} {
	outbound["type"] = "failover"
	delete(outbound, "url")
	delete(outbound, "interval")
	delete(outbound, "tolerance")
	members := stringList(outbound["outbounds"])
	if len(members) > 0 && strings.TrimSpace(fmt.Sprint(outbound["default"])) == "" {
		outbound["default"] = members[0]
	}
	probeTarget := "http://www.gstatic.com/generate_204"
	if failover, ok := outbound["failover"].(map[string]interface{}); ok {
		if value := strings.TrimSpace(fmt.Sprint(failover["probe_target"])); value != "" && value != "<nil>" {
			probeTarget = value
		}
	}
	outbound["failover"] = map[string]interface{}{
		"enabled":      true,
		"probe_target": probeTarget,
		"interval":     "10m",
		"hysteresis":   2,
	}
	return outbound
}
func stringList(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []interface{}:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := strings.TrimSpace(fmt.Sprint(item)); value != "" && value != "<nil>" {
				result = append(result, value)
			}
		}
		return result
	default:
		return nil
	}
}
