//go:build !minimal

// Package settings owns Remote Outbound Subscriptions setting keys and defaults.
package settings

import (
	subconversion "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/conversion"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

const (
	GroupAdaptationKey  = "subRemoteGroupAdaptation"
	ConversionPolicyKey = "subRemoteConversionPolicy"
)

func Defaults() map[string]string {
	return map[string]string{
		GroupAdaptationKey:  "urltest",
		ConversionPolicyKey: subconversion.DefaultPolicyJSON(),
	}
}

func AllKeys() []string {
	return []string{
		GroupAdaptationKey,
		ConversionPolicyKey,
	}
}

type Reader struct{}

func (Reader) GetRemoteGroupAdaptation() (string, error) {
	return (&service.SettingService{}).GetComponentSettingString(GroupAdaptationKey)
}

func (Reader) GetRemoteConversionPolicy() (string, error) {
	return (&service.SettingService{}).GetComponentSettingString(ConversionPolicyKey)
}
