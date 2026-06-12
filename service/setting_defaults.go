package service

import (
	"sort"

	"github.com/MalenkiySolovey/solovey-ui/config"
)

var defaultValueMap = mergeSettingDefaultMaps(
	defaultWebSettingValues,
	defaultSessionSettingValues,
	defaultRuntimeSettingValues,
	defaultSubscriptionSettingValues,
	defaultTelegramSettingValues,
	defaultPaidSubSettingValues,
	defaultInternalSettingValues,
)

var internalSettingKeys = settingKeySet(
	"secret",
	"installSalt",
	"sessionGeneration",
	"config",
	"version",
	settingKeyPaidSubUpdateOffset,
)

func defaultSettingKeys() []string {
	keys := make([]string, 0, len(defaultValueMap))
	for key := range defaultValueMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func defaultSettingValue(key string) (string, bool) {
	value, ok := defaultValueMap[key]
	if !ok {
		return "", false
	}
	if key == "version" {
		return config.GetVersion(), true
	}
	return value, true
}

func hideInternalSettings(settings map[string]string) {
	for key := range internalSettingKeys {
		delete(settings, key)
	}
}

func isEditableSettingKey(key string) bool {
	if _, ok := defaultValueMap[key]; !ok {
		return false
	}
	_, internal := internalSettingKeys[key]
	return !internal
}

func mergeSettingDefaultMaps(groups ...map[string]string) map[string]string {
	merged := make(map[string]string)
	for _, group := range groups {
		for key, value := range group {
			if _, exists := merged[key]; exists {
				panic("duplicate default setting key: " + key)
			}
			merged[key] = value
		}
	}
	return merged
}

func settingKeySet(keys ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return set
}
