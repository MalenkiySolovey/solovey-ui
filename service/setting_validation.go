package service

import (
	"os"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"
	settingsvalidation "github.com/MalenkiySolovey/solovey-ui/internal/settings/validation"
)

func (s *SettingService) validateAll(settings map[string]string) error {
	if err := s.validateSubscriptionPathSettings(settings); err != nil {
		return err
	}
	for _, key := range settingcatalog.SortedKeys(settings) {
		obj := settings[key]
		if settingsschema.IsSecretPresenceMarker(key) {
			continue
		}
		if err := s.validateSettingInput(key, obj); err != nil {
			return err
		}
	}
	return nil
}

func (s *SettingService) validateSettingInput(key string, value string) error {
	if err := settingsvalidation.ValidateProxyURLSetting(key, value, StoredSecretMarker); err != nil {
		return err
	}
	if err := settingsvalidation.ValidateTelegramSettingInput(key, value, StoredSecretMarker); err != nil {
		return err
	}
	if err := settingsvalidation.ValidateSessionSettingInput(key, value); err != nil {
		return err
	}
	if err := settingsvalidation.ValidateRuntimeSettingInput(key, value); err != nil {
		return err
	}
	if err := settingsvalidation.ValidateSubscriptionSettingInput(key, value); err != nil {
		return err
	}
	if err := settingsvalidation.ValidatePaidSubSettingInput(key, value); err != nil {
		return err
	}
	if err := settingsvalidation.ValidateIPCertSettingInput(key, value); err != nil {
		return err
	}
	return s.validateEndpointSettingInput(key, value)
}

func (s *SettingService) validateEndpointSettingInput(key string, value string) error {
	return settingsvalidation.ValidateEndpointSettingInput(key, value, s.fileExists)
}

func (s *SettingService) fileExists(path string) error {
	_, err := os.Stat(path)
	return err
}

func (s *SettingService) validateSubscriptionPathSettings(settings map[string]string) error {
	touched := false
	for _, key := range subscriptionPathSettingKeys {
		if _, ok := settings[key]; ok {
			touched = true
			break
		}
	}
	if !touched {
		return nil
	}

	paths := make(map[string]string, len(subscriptionPathSettingKeys))
	for _, key := range subscriptionPathSettingKeys {
		value, ok := settings[key]
		if !ok {
			var err error
			value, err = s.getString(key)
			if err != nil {
				return err
			}
		}
		normalized, err := settingsvalidation.NormalizeAndValidatePathSetting(key, value)
		if err != nil {
			return err
		}
		paths[key] = normalized
	}

	return settingsvalidation.ValidateSubscriptionPaths(settingsvalidation.SubscriptionPaths{
		Base:  paths[settingKeySubPath],
		JSON:  paths[settingKeySubJsonPath],
		Clash: paths[settingKeySubClashPath],
		Xray:  paths[settingKeySubXrayPath],
	})
}
