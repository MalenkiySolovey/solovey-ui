package remote

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

type NormalizedOutbound struct {
	Name      string
	Type      string
	SourceKey string
	SortOrder int
	Options   json.RawMessage
	Canonical json.RawMessage
}

func NormalizeOutbound(outbound map[string]interface{}, index int) (NormalizedOutbound, error) {
	return NormalizeOutboundWithCanonical(outbound, index, nil)
}

func NormalizeOutboundWithCanonical(outbound map[string]interface{}, index int, canonicalConnection *subcanonical.Connection) (NormalizedOutbound, error) {
	raw := CloneOutboundMap(outbound)
	outboundType, _ := raw["type"].(string)
	outboundType = strings.TrimSpace(outboundType)
	if outboundType == "" {
		return NormalizedOutbound{}, common.NewError("subscription outbound has no type")
	}
	name := DisplayName(raw, index)
	sourceKey, err := SourceKey(raw, index)
	if err != nil {
		return NormalizedOutbound{}, err
	}
	canonicalValue := subcanonical.ObserveOutbound(subcanonical.FormatSingBox, raw, index)
	if canonicalConnection != nil {
		canonicalValue = *canonicalConnection
	}
	canonicalData, err := json.Marshal(canonicalValue)
	if err != nil {
		return NormalizedOutbound{}, err
	}
	optionsRaw := subcanonical.CleanOutbound(CloneOutboundMap(raw))
	delete(optionsRaw, "id")
	delete(optionsRaw, "sortOrder")
	delete(optionsRaw, "sort_order")
	delete(optionsRaw, "type")
	delete(optionsRaw, "tag")
	options, err := json.MarshalIndent(optionsRaw, "", "  ")
	if err != nil {
		return NormalizedOutbound{}, err
	}
	return NormalizedOutbound{
		Name:      name,
		Type:      outboundType,
		SourceKey: sourceKey,
		SortOrder: index + 1,
		Options:   options,
		Canonical: canonicalData,
	}, nil
}

func UpdateConnection(connection *model.RemoteOutboundConnection, normalized NormalizedOutbound, now int64) bool {
	changed := false
	if connection.Name != normalized.Name {
		connection.Name = normalized.Name
		changed = true
	}
	if connection.Type != normalized.Type {
		connection.Type = normalized.Type
		changed = true
	}
	if normalized.SortOrder > 0 && connection.SortOrder != normalized.SortOrder {
		connection.SortOrder = normalized.SortOrder
		changed = true
	}
	if !JSONRawEqual(connection.Options, normalized.Options) {
		connection.Options = normalized.Options
		changed = true
	}
	if len(normalized.Canonical) > 0 && !JSONRawEqual(connection.Canonical, normalized.Canonical) {
		connection.Canonical = normalized.Canonical
		changed = true
	}
	if connection.Missing {
		connection.Missing = false
		connection.MissingReason = ""
		connection.MissingSince = 0
		changed = true
	}
	connection.LastSeen = now
	connection.UpdatedAt = now
	return changed
}

func SourceKey(outbound map[string]interface{}, index int) (string, error) {
	for _, key := range []string{"tag", "name", "remarks", "remark", "ps"} {
		if value, ok := outbound[key].(string); ok && strings.TrimSpace(value) != "" {
			return labelSourceKey(value), nil
		}
	}
	if name := DisplayName(outbound, index); strings.TrimSpace(name) != "" {
		return labelSourceKey(name), nil
	}
	return "", common.NewError("subscription outbound has no stable display name")
}

func labelSourceKey(value string) string {
	return "label:" + subcanonical.NormalizeLabel(value)
}

func legacyTypedLabelSourceKey(outboundType string, sourceKey string) string {
	const prefix = "label:"
	if !strings.HasPrefix(sourceKey, prefix) {
		return ""
	}
	label := strings.TrimPrefix(sourceKey, prefix)
	outboundType = subcanonical.NormalizeLabel(outboundType)
	if label == "" || outboundType == "" {
		return ""
	}
	return prefix + outboundType + ":" + label
}

func UniqueSourceKey(seen map[string]struct{}, sourceKey string) string {
	if _, ok := seen[sourceKey]; !ok {
		return sourceKey
	}
	for index := 2; ; index++ {
		candidate := fmt.Sprintf("%s:%d", sourceKey, index)
		if _, ok := seen[candidate]; !ok {
			return candidate
		}
	}
}

func DisplayName(outbound map[string]interface{}, index int) string {
	for _, key := range []string{"tag", "name", "remarks", "remark", "ps"} {
		if value, ok := outbound[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	server, _ := outbound["server"].(string)
	port := PortString(outbound["server_port"])
	if server != "" && port != "" {
		return server + ":" + port
	}
	outboundType, _ := outbound["type"].(string)
	outboundType = strings.TrimSpace(outboundType)
	if outboundType == "" {
		outboundType = "outbound"
	}
	return fmt.Sprintf("%s-%d", outboundType, index+1)
}

func PortString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case uint:
		return fmt.Sprintf("%d", v)
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func CloneOutboundMap(input map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func CloneRawMessage(input json.RawMessage) json.RawMessage {
	if input == nil {
		return nil
	}
	cloned := make([]byte, len(input))
	copy(cloned, input)
	return cloned
}

func JSONRawEqual(left json.RawMessage, right json.RawMessage) bool {
	var leftValue any
	var rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return string(left) == string(right)
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return string(left) == string(right)
	}
	leftBytes, _ := json.Marshal(leftValue)
	rightBytes, _ := json.Marshal(rightValue)
	return string(leftBytes) == string(rightBytes)
}

func DefaultTagPrefix(name string, id uint) string {
	base := SanitizeTagPart(name)
	if base == "" {
		base = "remote"
	}
	if id != 0 {
		return fmt.Sprintf("ros%d-%s-", id, base)
	}
	return "ros{id}-" + base + "-"
}

func SanitizeTagPrefix(value string) string {
	value = SanitizeTagText(value)
	if value == "" {
		return ""
	}
	value = strings.Trim(value, "-_. ")
	if value == "" {
		return ""
	}
	if !strings.HasSuffix(value, "-") && !strings.HasSuffix(value, "_") && !strings.HasSuffix(value, ".") {
		value += "-"
	}
	return TrimTag(value)
}

func SanitizeTagPart(value string) string {
	value = strings.Trim(SanitizeTagText(value), "-_. ")
	if value == "" {
		return "connection"
	}
	return value
}

func SanitizeTagText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	lastWasDash := false
	for _, r := range value {
		switch {
		case unicode.IsControl(r):
			continue
		case unicode.IsSpace(r):
			if !lastWasDash {
				builder.WriteRune('-')
				lastWasDash = true
			}
		default:
			builder.WriteRune(r)
			lastWasDash = r == '-'
		}
	}
	return strings.Trim(builder.String(), "-")
}

func TrimTag(value string) string {
	const maxTagLength = 96
	value = strings.TrimFunc(value, unicode.IsSpace)
	runes := []rune(value)
	if len(runes) <= maxTagLength {
		return value
	}
	return strings.TrimRightFunc(string(runes[:maxTagLength]), func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || unicode.IsSpace(r)
	})
}

func NormalizeUpdateInterval(value int64) int64 {
	const defaultInterval = int64(24 * 60 * 60)
	const minInterval = int64(5 * 60)
	if value <= 0 {
		return defaultInterval
	}
	if value < minInterval {
		return minInterval
	}
	return value
}
