package service

import (
	"sort"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func (s *SettingService) validateSaveKeys(settings map[string]string) error {
	for _, key := range sortedSettingKeys(settings) {
		if strings.HasSuffix(key, "HasSecret") {
			baseKey := strings.TrimSuffix(key, "HasSecret")
			if isEncryptedSettingKey(baseKey) {
				continue
			}
			return common.NewError("invalid setting key: ", key)
		}
		if !isEditableSettingKey(key) {
			return common.NewError("invalid setting key: ", key)
		}
	}
	return nil
}

func (s *SettingService) validateAll(settings map[string]string) error {
	if err := s.validateSubscriptionPathSettings(settings); err != nil {
		return err
	}
	for _, key := range sortedSettingKeys(settings) {
		obj := settings[key]
		if shouldSkipSettingValidationKey(key) {
			continue
		}
		if err := s.validateSettingInput(key, obj); err != nil {
			return err
		}
	}
	return nil
}

func sortedSettingKeys(settings map[string]string) []string {
	keys := make([]string, 0, len(settings))
	for key := range settings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func shouldSkipSettingValidationKey(key string) bool {
	return strings.HasSuffix(key, "HasSecret")
}

func (s *SettingService) validateSettingInput(key string, value string) error {
	if err := validateProxyURLSetting(key, value); err != nil {
		return err
	}
	if err := validateTelegramSettingInput(key, value); err != nil {
		return err
	}
	if err := validateSessionSettingInput(key, value); err != nil {
		return err
	}
	if err := validateRuntimeSettingInput(key, value); err != nil {
		return err
	}
	if err := validateSubscriptionSettingInput(key, value); err != nil {
		return err
	}
	if err := validatePaidSubSettingInput(key, value); err != nil {
		return err
	}
	return s.validateEndpointSettingInput(key, value)
}

func validateProxyURLSetting(key string, value string) error {
	if key != settingKeyTelegramProxyURL && key != settingKeyPaidSubProxyURL {
		return nil
	}
	if value == "" || value == StoredSecretMarker {
		return nil
	}
	return validateTelegramProxyURL(value)
}
