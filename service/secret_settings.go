package service

import (
	"strconv"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	settingscrypto "github.com/MalenkiySolovey/solovey-ui/internal/settings/crypto"
	settingsmanager "github.com/MalenkiySolovey/solovey-ui/internal/settings/manager"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
)

var encryptedSettingKeys = settingcatalog.MergeKeySets(telegramEncryptedSettingKeys, paidSubEncryptedSettingKeys, ipCertEncryptedSettingKeys)

// #nosec G101 -- UI placeholder text shown in place of a stored secret, not a credential.
const StoredSecretMarker = "\u2022\u2022\u2022 stored \u2022\u2022\u2022"

var (
	cookieKeyHKDFInfo            = settingscrypto.CookieKeyHKDFInfo
	settingsSecretboxKeyHKDFInfo = settingscrypto.SettingsSecretboxKeyHKDFInfo
)

type secretboxCandidate struct {
	name string
	box  *secretbox.Box
}

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
	if key == settingKeyTelegramBackupPassphrase {
		if value == "" {
			settings[key] = ""
		} else {
			settings[key] = StoredSecretMarker
		}
	}
}

func (s *SettingService) getSecretboxCandidates() ([]secretboxCandidate, error) {
	candidates, err := s.settingsSecretCodec().SecretboxCandidates()
	if err != nil {
		return nil, err
	}
	return fromSettingsCryptoCandidates(candidates), nil
}

func (s *SettingService) GetCookieKeys() ([][]byte, error) {
	return s.settingsSecretCodec().CookieKeys()
}

func deriveHKDFKey(masterKey []byte, salt []byte, info []byte) ([]byte, error) {
	return settingscrypto.DeriveHKDFKey(masterKey, salt, info)
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

func decryptWithCandidate(candidates []secretboxCandidate, key, value string) (int, string, bool) {
	return settingscrypto.DecryptWithCandidate(toSettingsCryptoCandidates(candidates), key, value)
}

func (s *SettingService) ResealSecretSettings() (int, error) {
	return settingsmanager.ResealSecretSettings(settingsDatabase(), s.settingsSecretCodec(), encryptedSettingKeys)
}

func fromSettingsCryptoCandidates(candidates []settingscrypto.Candidate) []secretboxCandidate {
	converted := make([]secretboxCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		converted = append(converted, secretboxCandidate{
			name: candidate.Name,
			box:  candidate.Box,
		})
	}
	return converted
}

func toSettingsCryptoCandidates(candidates []secretboxCandidate) []settingscrypto.Candidate {
	converted := make([]settingscrypto.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		converted = append(converted, settingscrypto.Candidate{
			Name: candidate.name,
			Box:  candidate.box,
		})
	}
	return converted
}
