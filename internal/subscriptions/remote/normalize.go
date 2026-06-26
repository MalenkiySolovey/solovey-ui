package remote

import (
	"strings"
)

func cloneOutboundMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	output := make(map[string]interface{}, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
func normalizeRuntimeOutbound(outbound map[string]any) {
	if outbound == nil {
		return
	}
	normalizeRuntimeTLS(outbound)
}
func normalizeRuntimeTLS(outbound map[string]any) {
	rawTLS, ok := outbound["tls"]
	if !ok {
		return
	}
	tls, ok := rawTLS.(map[string]any)
	if !ok {
		return
	}
	if boolMapValue(tls["enabled"]) {
		return
	}
	delete(tls, "enabled")
	if runtimeTLSHasSignal(tls) {
		tls["enabled"] = true
		return
	}
	delete(tls, "utls")
	if len(tls) == 0 {
		delete(outbound, "tls")
	}
}
func runtimeTLSHasSignal(tls map[string]any) bool {
	for key, value := range tls {
		if key == "utls" || emptyRuntimeTLSValue(value) {
			continue
		}
		return true
	}
	return false
}
func emptyRuntimeTLSValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case bool:
		return !typed
	case string:
		value := strings.TrimSpace(typed)
		return value == "" || value == "<nil>"
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}
func boolMapValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}
