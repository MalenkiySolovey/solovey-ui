package parser

import (
	"strings"

	subconversion "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/conversion"
)

const (
	GroupAdaptationURLTest  = "urltest"
	GroupAdaptationSelector = "selector"
	GroupAdaptationFailover = "failover"
)

type ParseOptions struct {
	GroupAdaptation  string
	ConversionPolicy subconversion.Policy
}

func NormalizeGroupAdaptation(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case GroupAdaptationSelector:
		return GroupAdaptationSelector
	case GroupAdaptationFailover:
		return GroupAdaptationFailover
	default:
		return GroupAdaptationURLTest
	}
}

func unsupportedGroupTarget(options ParseOptions) string {
	if explicit := explicitGroupAdaptation(options.GroupAdaptation); explicit != "" {
		return explicit
	}
	return NormalizeGroupAdaptation(options.GroupAdaptation)
}

func conversionTarget(options ParseOptions, feature string) string {
	if feature == "" {
		return unsupportedGroupTarget(options)
	}
	if !hasConversionPolicy(options.ConversionPolicy) {
		if explicit := explicitGroupAdaptation(options.GroupAdaptation); explicit != "" {
			return explicit
		}
		return subconversion.DefaultPolicy().Mode(subconversion.TargetOutbound, feature)
	}
	return options.ConversionPolicy.Mode(subconversion.TargetOutbound, feature)
}

func hasConversionPolicy(policy subconversion.Policy) bool {
	return policy.Outbound != nil ||
		policy.Client.SingBox != nil ||
		policy.Client.Xray != nil ||
		policy.Client.Mihomo != nil
}

func explicitGroupAdaptation(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return NormalizeGroupAdaptation(value)
}
