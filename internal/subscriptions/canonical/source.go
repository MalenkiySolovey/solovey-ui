package canonical

import "encoding/json"

const sourceMetadataKey = "_subscription_source"

// CaptureSource keeps the original parser input in the existing observation
// snapshot. It is neither a runtime option nor a second editable resource.
func CaptureSource(outbound map[string]any, format string, native map[string]any, diagnostics ...ProjectionDiagnostic) {
	if outbound == nil {
		return
	}
	encoded, _ := json.Marshal(diagnostics)
	var values []any
	_ = json.Unmarshal(encoded, &values)
	outbound[sourceMetadataKey] = map[string]any{"format": format, "options": cloneMap(native), "diagnostics": values}
}

func sourceObservation(outbound map[string]any) (map[string]any, []ProjectionDiagnostic) {
	source, _ := outbound[sourceMetadataKey].(map[string]any)
	if source == nil {
		return nil, nil
	}
	encoded, _ := json.Marshal(source["diagnostics"])
	var diagnostics []ProjectionDiagnostic
	_ = json.Unmarshal(encoded, &diagnostics)
	return cloneMap(source), diagnostics
}
