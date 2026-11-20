package service

import (
	settingsmanager "github.com/MalenkiySolovey/solovey-backend/settings/manager"
	settingsvalidation "github.com/MalenkiySolovey/solovey-backend/settings/validation"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
)

func (s *SettingService) settingsManager(auditFallback ...bool) settingsmanager.Manager {
	auditSecretFallback := len(auditFallback) > 0 && auditFallback[0]
	return settingsmanager.Manager{
		DB:           s.settingDatabase,
		Schema:       currentSettingsSchema(),
		DefaultValue: defaultSettingValue,
		Secret:       settingSecretCodec{service: s, auditFallback: auditSecretFallback},
		StoredSecret: StoredSecretMarker,
		Hooks: settingsmanager.Hooks{
			ValidateAll: s.validateAll,
			CanSaveKey:  enabledstate.IsMutableSettingKey,
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
