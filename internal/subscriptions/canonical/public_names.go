package canonical

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"
)

const PublicRemarkKey = "publicRemark"

// ClientPublicRemark reads only explicit public metadata, never the private
// client description or authentication name. Existing client JSON owns storage.
func ClientPublicRemark(config json.RawMessage) (string, error) {
	if !utf8.Valid(config) {
		return "", errors.New("invalid subscription client config encoding")
	}
	if len(config) == 0 || string(config) == "null" {
		return "", nil
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(config, &root) != nil {
		return "", errors.New("invalid subscription client config")
	}
	raw, present := root[MetadataKey]
	if !present {
		return "", nil
	}
	var metadata map[string]json.RawMessage
	if json.Unmarshal(raw, &metadata) != nil || metadata == nil {
		return "", errors.New("subscription public metadata must be an object")
	}
	raw, present = metadata[PublicRemarkKey]
	if !present {
		return "", nil
	}
	var remark string
	if json.Unmarshal(raw, &remark) != nil || string(raw) == "null" || !utf8.ValidString(remark) || len(remark) > 512 || utf8.RuneCountInString(remark) > 128 || containsControl(remark) {
		return "", errors.New("subscription public remark must be text of at most 128 characters without controls")
	}
	return remark, nil
}

func containsControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// RetainClientPublicMetadata preserves an optional public remark when a legacy
// editor omits its metadata. An explicit metadata object (including {}) wins.
func RetainClientPublicMetadata(incoming, stored json.RawMessage) (json.RawMessage, error) {
	if _, err := ClientPublicRemark(stored); err != nil {
		return nil, err
	}
	var previous, current map[string]json.RawMessage
	if len(stored) > 0 && json.Unmarshal(stored, &previous) != nil {
		return nil, errors.New("invalid stored subscription metadata")
	}
	metadata, present := previous[MetadataKey]
	if !present {
		return incoming, nil
	}
	if len(incoming) > 0 && json.Unmarshal(incoming, &current) != nil {
		return nil, errors.New("invalid subscription client config")
	}
	if _, explicit := current[MetadataKey]; explicit {
		return incoming, nil
	}
	if current == nil {
		current = map[string]json.RawMessage{}
	}
	current[MetadataKey] = metadata
	return json.Marshal(current)
}

// UniqueLabels preserves literal names, including future suffix-shaped names.
// Only real collisions receive the first free deterministic suffix.
func UniqueLabels(labels []string, reserved ...string) []string {
	literals, used, protected := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, label := range labels {
		literals[label] = true
	}
	for _, label := range reserved {
		protected[label] = true
	}
	result := make([]string, len(labels))
	for i, base := range labels {
		name := base
		if used[name] || protected[name] {
			for suffix := 2; ; suffix++ {
				name = fmt.Sprintf("%s-%d", base, suffix)
				if !used[name] && !protected[name] && !literals[name] {
					break
				}
			}
		}
		used[name] = true
		result[i] = name
	}
	return result
}

// CloneOutbound preserves adapter metadata while isolating nested projection
// changes. CleanOutbound remains the separate public runtime stripping owner.
func CloneOutbound(outbound map[string]any) map[string]any { return cloneMap(outbound) }

// NamedOutbounds allocates the complete projection namespace before changing
// references. An ambiguous duplicate source label resolves to its first node.
func NamedOutbounds(outbounds []map[string]any, reserved ...string) ([]map[string]any, []string) {
	labels := make([]string, len(outbounds))
	for i, outbound := range outbounds {
		labels[i], _ = outbound["tag"].(string)
	}
	names := UniqueLabels(labels, reserved...)
	first := make(map[string]string, len(labels))
	for i, label := range labels {
		if first[label] == "" {
			first[label] = names[i]
		}
	}
	result := make([]map[string]any, len(outbounds))
	for i, outbound := range outbounds {
		result[i] = CloneOutbound(outbound)
		result[i]["tag"] = names[i]
		RenameOutboundReferences(result[i], first)
	}
	return result, names
}

// RenameOutboundReferences updates only existing typed graph-reference fields.
func RenameOutboundReferences(outbound map[string]any, names map[string]string) {
	for _, key := range []string{"detour", "default"} {
		if old, ok := outbound[key].(string); ok && names[old] != "" {
			outbound[key] = names[old]
		}
	}
	switch refs := outbound["outbounds"].(type) {
	case []string:
		updated := append([]string(nil), refs...)
		for i, old := range updated {
			if names[old] != "" {
				updated[i] = names[old]
			}
		}
		outbound["outbounds"] = updated
	case []any:
		updated := append([]any(nil), refs...)
		for i, ref := range updated {
			if old, ok := ref.(string); ok && names[old] != "" {
				updated[i] = names[old]
			}
		}
		outbound["outbounds"] = updated
	}
}
