package remotesubservice

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

type profileField struct {
	Key   string
	Label string
	Value string
}

func flattenObservationFields(value any, prefix string) []profileField {
	values, ok := value.(map[string]any)
	if !ok || len(values) == 0 {
		return nil
	}
	fields := make([]profileField, 0, len(values))
	for key, rawValue := range values {
		key = strings.TrimSpace(key)
		if key == "" || key == subcanonical.MetadataKey {
			continue
		}
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if skipProfileField(path) {
			continue
		}
		if nested, ok := rawValue.(map[string]any); ok && len(nested) > 0 {
			fields = append(fields, flattenObservationFields(nested, path)...)
			continue
		}
		normalizedKey, label := profileFieldName(path)
		displayValue := profileValueString(rawValue)
		if displayValue == "" {
			continue
		}
		fields = append(fields, profileField{
			Key:   normalizedKey,
			Label: label,
			Value: displayValue,
		})
	}
	return fields
}
func skipProfileField(path string) bool {
	switch path {
	case "xray_profile", "xray_profile_type", "xray_profile_outbounds", "xray_profile_balancers", "mihomo_group":
		return true
	default:
		return false
	}
}
func profileFieldName(path string) (string, string) {
	switch path {
	case "tag", "name", "remarks", "remark", "ps":
		return "name", "Name"
	case "xray_tag":
		return "xray_tag", "Xray tag"
	case "type":
		return "type", "Outbound type"
	case "server":
		return "server", "IP"
	case "server_port":
		return "server_port", "Port"
	case "uuid", "id":
		return "uuid", "UUID"
	case "tls.enabled":
		return "tls.enabled", "TLS"
	case "tls.server_name":
		return "tls.server_name", "SNI"
	case "tls.insecure":
		return "tls.insecure", "Allow insecure"
	case "tls.utls.fingerprint":
		return "tls.utls.fingerprint", "Fingerprint"
	case "tls.reality.enabled":
		return "tls.reality.enabled", "Reality"
	case "tls.reality.public_key":
		return "tls.reality.public_key", "Reality public key"
	case "tls.reality.short_id":
		return "tls.reality.short_id", "Reality short id"
	case "transport.type":
		return "transport.type", "Transport"
	case "transport.host", "transport.headers.Host":
		return "transport.host", "Host"
	case "transport.path":
		return "transport.path", "Path"
	case "outbounds":
		return "outbounds", "Members"
	default:
		return path, profileDefaultLabel(path)
	}
}
func profileDefaultLabel(path string) string {
	path = strings.ReplaceAll(path, "_", " ")
	parts := strings.Split(path, ".")
	for index := range parts {
		if parts[index] == "" {
			continue
		}
		parts[index] = strings.ToUpper(parts[index][:1]) + parts[index][1:]
	}
	return strings.Join(parts, " / ")
}
func profileValueString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case uint:
		return fmt.Sprintf("%d", typed)
	case []string:
		return strings.Join(typed, ", ")
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := profileValueString(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(typed))
		}
		return string(data)
	}
}
func addProfileCharacteristicValue(valuesByKey map[string]map[string]map[string]struct{}, labelsByKey map[string]string, key string, label string, value string, source string) {
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return
	}
	if valuesByKey[key] == nil {
		valuesByKey[key] = map[string]map[string]struct{}{}
	}
	if valuesByKey[key][value] == nil {
		valuesByKey[key][value] = map[string]struct{}{}
	}
	valuesByKey[key][value][source] = struct{}{}
	labelsByKey[key] = label
}
func profileCharacteristicsFromValues(valuesByKey map[string]map[string]map[string]struct{}, labelsByKey map[string]string) []CollectedProfileCharacteristic {
	keys := make([]string, 0, len(valuesByKey))
	for key := range valuesByKey {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		return characteristicOrder(keys[i]) < characteristicOrder(keys[j]) ||
			(characteristicOrder(keys[i]) == characteristicOrder(keys[j]) && labelsByKey[keys[i]] < labelsByKey[keys[j]])
	})
	result := make([]CollectedProfileCharacteristic, 0, len(keys))
	for _, key := range keys {
		values := make([]CollectedProfileValue, 0, len(valuesByKey[key]))
		for value, sources := range valuesByKey[key] {
			values = append(values, CollectedProfileValue{
				Value:   value,
				Sources: sortedSourceSet(sources),
			})
		}
		sort.SliceStable(values, func(i, j int) bool {
			return values[i].Value < values[j].Value
		})
		result = append(result, CollectedProfileCharacteristic{
			Key:    key,
			Label:  labelsByKey[key],
			Values: values,
		})
	}
	return result
}
func profileMapList(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if value, ok := item.(map[string]any); ok {
				result = append(result, value)
			}
		}
		return result
	default:
		return nil
	}
}
func profileBoolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}
func profileConnectionSources(connection subcanonical.Connection) []string {
	sources := map[string]struct{}{}
	for _, format := range connection.Formats {
		sources[sourceLabel(format)] = struct{}{}
	}
	for _, observation := range connection.Observations {
		sources[sourceLabel(observation.Format)] = struct{}{}
	}
	for _, adaptation := range connection.Adaptations {
		if adaptation.SourceFormat != "" {
			sources[sourceLabel(adaptation.SourceFormat)] = struct{}{}
		}
	}
	return sortedSourceSet(sources)
}
func sourceLabel(format string) string {
	switch strings.TrimSpace(format) {
	case subcanonical.FormatXray:
		return "x-ray"
	case subcanonical.FormatClash:
		return "mihomo"
	case subcanonical.FormatSingBox:
		return "sing-box"
	case subcanonical.FormatURI:
		return "uri"
	case "":
		return "unknown"
	default:
		return strings.TrimSpace(format)
	}
}
func sourceSuffix(sources []string) string {
	if len(sources) == 0 {
		return ""
	}
	return " [" + strings.Join(sources, ", ") + "]"
}
func sortedSourceSet(sources map[string]struct{}) []string {
	result := make([]string, 0, len(sources))
	for source := range sources {
		if strings.TrimSpace(source) != "" {
			result = append(result, source)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		return sourceOrder(result[i]) < sourceOrder(result[j]) ||
			(sourceOrder(result[i]) == sourceOrder(result[j]) && result[i] < result[j])
	})
	return result
}
func sourceOrder(source string) int {
	switch source {
	case "x-ray":
		return 10
	case "mihomo":
		return 20
	case "sing-box":
		return 30
	case "uri":
		return 40
	default:
		return 100
	}
}
func characteristicOrder(key string) int {
	order := map[string]int{
		"name":                   10,
		"xray_tag":               20,
		"type":                   30,
		"server":                 40,
		"server_port":            50,
		"uuid":                   60,
		"password":               70,
		"method":                 80,
		"flow":                   90,
		"network":                100,
		"tls.enabled":            110,
		"tls.server_name":        120,
		"tls.insecure":           130,
		"tls.utls.fingerprint":   140,
		"tls.reality.enabled":    150,
		"transport.type":         170,
		"transport.host":         180,
		"transport.path":         190,
		"xray_balancer":          200,
		"xray_balancer.selector": 210,
		"xray_balancer.members":  220,
		"xray_balancer.fallback": 230,
		"xray_balancer.strategy": 240,
		"outbounds":              300,
	}
	if value, ok := order[key]; ok {
		return value
	}
	return 1000
}
