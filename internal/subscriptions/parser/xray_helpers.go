package parser

import (
	"strings"
)

func xrayScopedTag(scope string, tag string) string {
	scope = strings.TrimSpace(scope)
	tag = strings.TrimSpace(tag)
	if scope == "" || tag == "" {
		return tag
	}
	return scope + " / " + tag
}
func xrayList(value interface{}) []interface{} {
	switch typed := value.(type) {
	case []interface{}:
		return typed
	default:
		return nil
	}
}
func xrayStringList(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := strings.TrimSpace(stringValue(item)); value != "" {
				result = append(result, value)
			}
		}
		return result
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{strings.TrimSpace(typed)}
	default:
		return nil
	}
}
func appendUniqueStrings(values []string, next ...string) []string {
	for _, candidate := range next {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		exists := false
		for _, existing := range values {
			if existing == candidate {
				exists = true
				break
			}
		}
		if !exists {
			values = append(values, candidate)
		}
	}
	return values
}
func cloneXrayMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	output := make(map[string]interface{}, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
func cloneXrayMaps(input []map[string]interface{}) []map[string]interface{} {
	if len(input) == 0 {
		return nil
	}
	output := make([]map[string]interface{}, 0, len(input))
	for _, item := range input {
		output = append(output, cloneXrayMap(item))
	}
	return output
}
