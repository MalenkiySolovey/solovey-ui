package service

import (
	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	settingsmanager "github.com/MalenkiySolovey/solovey-ui/internal/settings/manager"
	settingsvalidation "github.com/MalenkiySolovey/solovey-ui/internal/settings/validation"
)

func (s *SettingService) settingsManager(auditFallback ...bool) settingsmanager.Manager {
	auditSecretFallback := len(auditFallback) > 0 && auditFallback[0]
	return settingsmanager.Manager{
		DB:           settingsDatabase,
		Schema:       currentSettingsSchema(),
		DefaultValue: defaultSettingValue,
		Secret:       settingSecretCodec{service: s, auditFallback: auditSecretFallback},
		StoredSecret: StoredSecretMarker,
		Hooks: settingsmanager.Hooks{
			ValidateAll: s.validateAll,
			CanSaveKey:  enabledstate.IsSettingKey,
			NormalizePath: func(key string, value string) (string, bool, error) {
				if !settingsvalidation.IsPathSetting(key) {
					return value, false, nil
				}
				normalized, err := settingsvalidation.NormalizeAndValidatePathSetting(key, value)
				return normalized, true, err
			},
			ApplySideEffects:       applySettingSaveSideEffects,
			CanClearEmptyEncrypted: canClearEmptyEncryptedSetting,
		},
	}
}
