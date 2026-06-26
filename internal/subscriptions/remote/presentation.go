package remote

import (
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func HydrateConnectionTypeInfo(subscriptions []model.RemoteOutboundSubscription) {
	for subscriptionIndex := range subscriptions {
		for connectionIndex := range subscriptions[subscriptionIndex].Connections {
			connection := &subscriptions[subscriptionIndex].Connections[connectionIndex]
			connection.ConvertedType = strings.TrimSpace(connection.Type)
			connection.SourceType = connectionSourceType(*connection)
			if connection.SourceType == "" {
				connection.SourceType = connection.ConvertedType
			}
		}
	}
}

func connectionSourceType(connection model.RemoteOutboundConnection) string {
	canonical := canonicalConnection(connection.Canonical)
	if canonical == nil {
		return strings.TrimSpace(connection.Type)
	}
	if sourceType := sourceTypeFromAdaptations(canonical.Adaptations); sourceType != "" {
		return sourceType
	}
	if sourceType := sourceTypeFromObservations(canonical.Observations); sourceType != "" {
		return sourceType
	}
	return strings.TrimSpace(canonical.Protocol)
}

func sourceTypeFromAdaptations(adaptations []subcanonical.Adaptation) string {
	for _, adaptation := range adaptations {
		sourceType := strings.TrimSpace(adaptation.SourceType)
		if sourceType == "" {
			sourceType = strings.TrimSpace(adaptation.TargetType)
		}
		if sourceType == "" {
			continue
		}
		return formatSourceType(adaptation.SourceFormat, sourceType)
	}
	return ""
}

func sourceTypeFromObservations(observations []subcanonical.Observation) string {
	for _, observation := range observations {
		if observation.Outbound == nil {
			continue
		}
		sourceType := strings.TrimSpace(fmt.Sprint(observation.Outbound["type"]))
		if sourceType == "" || sourceType == "<nil>" {
			continue
		}
		return formatSourceType(observation.Format, sourceType)
	}
	return ""
}

func formatSourceType(format string, sourceType string) string {
	sourceType = strings.TrimSpace(sourceType)
	if sourceType == "" {
		return ""
	}
	switch strings.TrimSpace(format) {
	case subcanonical.FormatClash:
		return "mihomo " + sourceType
	case subcanonical.FormatXray:
		return "xray " + sourceType
	case subcanonical.FormatSingBox:
		return "sing-box " + sourceType
	case subcanonical.FormatURI:
		return "uri " + sourceType
	default:
		return sourceType
	}
}
