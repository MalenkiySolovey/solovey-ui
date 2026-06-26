package formats

import (
	"strings"

	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func formatAdaptations(outbound map[string]interface{}) []subcanonical.Adaptation {
	if outbound == nil {
		return nil
	}
	return formatAdaptationsFromValue(outbound[subcanonical.MetadataKey])
}

func formatAdaptationsFromValue(value interface{}) []subcanonical.Adaptation {
	switch typed := value.(type) {
	case nil:
		return nil
	case subcanonical.Adaptation:
		return normalizeFormatAdaptation(typed)
	case []subcanonical.Adaptation:
		result := make([]subcanonical.Adaptation, 0, len(typed))
		for _, adaptation := range typed {
			result = append(result, normalizeFormatAdaptation(adaptation)...)
		}
		return result
	case map[string]interface{}:
		return normalizeFormatAdaptation(subcanonical.Adaptation{
			SourceFormat:  metadataText(typed, "source_format", "sourceFormat"),
			SourceFeature: metadataText(typed, "source_feature", "sourceFeature"),
			SourceType:    metadataText(typed, "source_type", "sourceType"),
			TargetType:    metadataText(typed, "target_type", "targetType"),
			Strategy:      metadataText(typed, "strategy"),
			Note:          metadataText(typed, "note"),
		})
	case []interface{}:
		result := make([]subcanonical.Adaptation, 0, len(typed))
		for _, item := range typed {
			result = append(result, formatAdaptationsFromValue(item)...)
		}
		return result
	default:
		return nil
	}
}

func normalizeFormatAdaptation(adaptation subcanonical.Adaptation) []subcanonical.Adaptation {
	adaptation.SourceFormat = strings.TrimSpace(adaptation.SourceFormat)
	adaptation.SourceFeature = strings.TrimSpace(adaptation.SourceFeature)
	adaptation.SourceType = strings.TrimSpace(adaptation.SourceType)
	adaptation.TargetType = strings.TrimSpace(adaptation.TargetType)
	adaptation.Strategy = strings.TrimSpace(adaptation.Strategy)
	adaptation.Note = strings.TrimSpace(adaptation.Note)
	if adaptation == (subcanonical.Adaptation{}) {
		return nil
	}
	return []subcanonical.Adaptation{adaptation}
}

func metadataText(value map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if result := strings.TrimSpace(asString(value[key])); result != "" {
			return result
		}
	}
	return ""
}
