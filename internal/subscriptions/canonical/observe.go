package canonical

import (
	"encoding/json"
	"fmt"
	"strings"
)

func ObserveOutbounds(format string, outbounds []map[string]any) Snapshot {
	snapshot := Snapshot{Version: SnapshotVersion}
	for index, outbound := range outbounds {
		if auxiliaryObservation(outbound) {
			snapshot.Extras = mergeObservations(snapshot.Extras, []Observation{observeExtra(format, outbound, index)})
			continue
		}
		connection := ObserveOutbound(format, outbound, index)
		snapshot = mergeSnapshotConnection(snapshot, connection)
	}
	return snapshot
}

func auxiliaryObservation(outbound map[string]any) bool {
	if outbound == nil {
		return false
	}
	return boolValue(outbound["_subscription_auxiliary"])
}

func observeExtra(format string, outbound map[string]any, index int) Observation {
	if strings.TrimSpace(format) == "" {
		format = FormatUnknown
	}
	clone := cloneMap(outbound)
	delete(clone, "_subscription_auxiliary")
	return Observation{
		Format:   format,
		Name:     displayName(clone, index),
		Outbound: clone,
	}
}

func ObserveOutbound(format string, outbound map[string]any, index int) Connection {
	if strings.TrimSpace(format) == "" {
		format = FormatUnknown
	}
	clone := cloneMap(outbound)
	adaptations := outboundAdaptations(clone)
	stripInternalMetadata(clone)
	name := displayName(clone, index)
	groupMembers := outboundGroupMembers(clone)
	connection := Connection{
		DisplayName: name,
		Kind:        outboundKind(groupMembers),
		Role:        outboundRole(clone),
		Protocol:    strings.TrimSpace(stringValue(clone["type"])),
		Endpoint: Endpoint{
			Server: strings.TrimSpace(stringValue(clone["server"])),
			Port:   portString(clone["server_port"]),
		},
		TLS:          tlsInfo(clone),
		Transport:    transportInfo(clone),
		GroupMembers: groupMembers,
		BestOutbound: cloneMap(clone),
		Formats:      []string{format},
		Adaptations:  adaptations,
		Observations: []Observation{{
			Format:   format,
			Name:     name,
			Outbound: cloneMap(clone),
		}},
	}
	return connection
}

func outboundKind(groupMembers []string) string {
	if len(groupMembers) > 0 {
		return KindGroup
	}
	return KindSingle
}

func outboundRole(outbound map[string]any) string {
	if boolValue(outbound["xray_profile_member"]) {
		return RoleMember
	}
	return RoleTopLevel
}

func CleanOutbound(outbound map[string]any) map[string]any {
	clone := cloneMap(outbound)
	stripInternalMetadata(clone)
	stripRuntimeMetadata(clone)
	return clone
}

func stripInternalMetadata(outbound map[string]any) {
	if outbound == nil {
		return
	}
	delete(outbound, MetadataKey)
	delete(outbound, "_subscription_metadata")
	delete(outbound, "_subscription_auxiliary")
}

func stripRuntimeMetadata(outbound map[string]any) {
	if outbound == nil {
		return
	}
	for key := range outbound {
		if strings.HasPrefix(key, "xray_") || strings.HasPrefix(key, "mihomo_") {
			delete(outbound, key)
		}
	}
}

func outboundAdaptations(outbound map[string]any) []Adaptation {
	if outbound == nil {
		return nil
	}
	return parseAdaptations(outbound[MetadataKey])
}

func parseAdaptations(value any) []Adaptation {
	switch typed := value.(type) {
	case nil:
		return nil
	case Adaptation:
		return normalizedAdaptations(typed)
	case map[string]any:
		return normalizedAdaptations(adaptationFromMap(typed))
	case []Adaptation:
		result := make([]Adaptation, 0, len(typed))
		for _, adaptation := range typed {
			result = append(result, normalizedAdaptations(adaptation)...)
		}
		return result
	case []map[string]any:
		result := make([]Adaptation, 0, len(typed))
		for _, item := range typed {
			result = append(result, normalizedAdaptations(adaptationFromMap(item))...)
		}
		return result
	case []any:
		result := make([]Adaptation, 0, len(typed))
		for _, item := range typed {
			result = append(result, parseAdaptations(item)...)
		}
		return result
	default:
		return nil
	}
}

func normalizedAdaptations(adaptation Adaptation) []Adaptation {
	adaptation.SourceFormat = strings.TrimSpace(adaptation.SourceFormat)
	adaptation.SourceFeature = strings.TrimSpace(adaptation.SourceFeature)
	adaptation.SourceType = strings.TrimSpace(adaptation.SourceType)
	adaptation.TargetType = strings.TrimSpace(adaptation.TargetType)
	adaptation.Strategy = strings.TrimSpace(adaptation.Strategy)
	adaptation.Note = strings.TrimSpace(adaptation.Note)
	if adaptation == (Adaptation{}) {
		return nil
	}
	return []Adaptation{adaptation}
}

func adaptationFromMap(value map[string]any) Adaptation {
	return Adaptation{
		SourceFormat:  metadataString(value, "source_format", "sourceFormat"),
		SourceFeature: metadataString(value, "source_feature", "sourceFeature"),
		SourceType:    metadataString(value, "source_type", "sourceType"),
		TargetType:    metadataString(value, "target_type", "targetType"),
		Strategy:      metadataString(value, "strategy"),
		Note:          metadataString(value, "note"),
	}
}

func metadataString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if result := strings.TrimSpace(stringValue(value[key])); result != "" {
			return result
		}
	}
	return ""
}

func outboundGroupMembers(outbound map[string]any) []string {
	members := stringList(outbound["outbounds"])
	if len(members) == 0 {
		return nil
	}
	result := make([]string, 0, len(members))
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		member = strings.TrimSpace(member)
		if member == "" {
			continue
		}
		if _, ok := seen[member]; ok {
			continue
		}
		seen[member] = struct{}{}
		result = append(result, member)
	}
	return result
}

func stringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := strings.TrimSpace(stringValue(item)); value != "" {
				result = append(result, value)
			}
		}
		return result
	default:
		return nil
	}
}

func tlsInfo(outbound map[string]any) TLS {
	tlsMap, ok := outbound["tls"].(map[string]any)
	if !ok {
		return TLS{}
	}
	info := TLS{
		Enabled:    boolValue(tlsMap["enabled"]),
		ServerName: strings.TrimSpace(stringValue(tlsMap["server_name"])),
	}
	if reality, ok := tlsMap["reality"].(map[string]any); ok {
		info.Reality = boolValue(reality["enabled"]) || strings.TrimSpace(stringValue(reality["public_key"])) != ""
	}
	return info
}

func transportInfo(outbound map[string]any) Transport {
	transportMap, ok := outbound["transport"].(map[string]any)
	if !ok {
		return Transport{}
	}
	info := Transport{
		Type: strings.TrimSpace(stringValue(transportMap["type"])),
		Path: firstString(transportMap["path"]),
	}
	if headers, ok := transportMap["headers"].(map[string]any); ok {
		info.Host = firstString(headers["Host"])
	}
	if info.Host == "" {
		info.Host = firstString(transportMap["host"])
	}
	return info
}

func displayName(outbound map[string]any, index int) string {
	for _, key := range []string{"tag", "name", "remarks", "remark", "ps"} {
		if value := strings.TrimSpace(stringValue(outbound[key])); value != "" {
			return value
		}
	}
	server := strings.TrimSpace(stringValue(outbound["server"]))
	port := portString(outbound["server_port"])
	if server != "" && port != "" {
		return server + ":" + port
	}
	protocol := strings.TrimSpace(stringValue(outbound["type"]))
	if protocol == "" {
		protocol = "connection"
	}
	return fmt.Sprintf("%s-%d", protocol, index+1)
}

func firstString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		for _, item := range typed {
			if value := strings.TrimSpace(stringValue(item)); value != "" {
				return value
			}
		}
	case []string:
		for _, item := range typed {
			if value := strings.TrimSpace(item); value != "" {
				return value
			}
		}
	}
	return ""
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return fmt.Sprintf("%.0f", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case uint:
		return fmt.Sprintf("%d", typed)
	default:
		return ""
	}
}

func portString(value any) string {
	return strings.TrimSpace(stringValue(value))
}

func boolValue(value any) bool {
	b, _ := value.(bool)
	return b
}
