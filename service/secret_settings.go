package service

import (
	"strconv"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	settingscrypto "github.com/MalenkiySolovey/solovey-ui/internal/settings/crypto"
	settingsmanager "github.com/MalenkiySolovey/solovey-ui/internal/settings/manager"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

var encryptedSettingKeys = settingcatalog.MergeKeySets(ipCertEncryptedSettingKeys)

// #nosec G101 -- UI placeholder text shown in place of a stored secret, not a credential.
const StoredSecretMarker = "\u2022\u2022\u2022 stored \u2022\u2022\u2022"

type settingSecretCodec struct {
	service       *SettingService
	auditFallback bool
}

func (c settingSecretCodec) EncryptString(key string, value string) (string, error) {
	return c.service.encryptSettingValue(key, value)
}

func (c settingSecretCodec) DecryptString(key string, value string) (string, error) {
	return c.service.settingsSecretCodec(c.auditFallback).DecryptString(key, value)
}

func (c settingSecretCodec) WriteMarker(settings map[string]string, key string, value string) {
	writeSecretSettingMarker(settings, key, value)
}

func (s *SettingService) settingsSecretCodec(auditFallback ...bool) settingscrypto.Codec {
	codec := settingscrypto.Codec{MasterSecret: s.GetSecret}
	if len(auditFallback) > 0 && auditFallback[0] {
		codec.AuditFallback = s.recordSecretboxFallback
	}
	return codec
}

func writeSecretSettingMarker(settings map[string]string, key string, value string) {
	settings[settingsschema.SecretPresenceKey(key)] = strconv.FormatBool(value != "")
	if canClearEmptyEncryptedSetting(key) {
		if value == "" {
			settings[key] = ""
		} else {
			settings[key] = StoredSecretMarker
		}
	}
}

func (s *SettingService) GetCookieKeys() ([][]byte, error) {
	return s.settingsSecretCodec().CookieKeys()
}

func (s *SettingService) encryptSettingValue(key string, value string) (string, error) {
	return s.settingsSecretCodec().EncryptString(key, value)
}

func (s *SettingService) decryptSettingValue(key string, value string) (string, error) {
	return s.settingsSecretCodec().DecryptString(key, value)
}

func (s *SettingService) decryptSettingBytes(key string, value string) ([]byte, error) {
	return s.settingsSecretCodec().DecryptBytes(key, value)
}

func (s *SettingService) recordSecretboxFallback(key string, candidate string) {
	if !settingsDatabaseAvailable() {
		return
	}
	if err := (&AuditService{}).Record(AuditEvent{
		Event:    "settings_secretbox_key_fallback",
		Resource: "settings",
		Severity: AuditSeverityWarn,
		Details: map[string]any{
			"key":       key,
			"candidate": candidate,
		},
	}); err != nil {
		logger.Warning("secretbox fallback audit failed:", err)
	}
}

func (s *SettingService) ResealSecretSettings() (int, error) {
	return settingsmanager.ResealSecretSettings(settingsDatabase(), s.settingsSecretCodec(), currentEncryptedSettingKeys())
}
