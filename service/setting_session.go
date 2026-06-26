package service

import (
	configsecurity "github.com/MalenkiySolovey/solovey-ui/config/security"
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/realtime"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func (s *SettingService) GetSecret() ([]byte, error) {
	setting, err := s.getSetting(settingcatalog.SecretKey)
	if settingNotFound(err) {
		secret, _ := defaultSettingValue(settingcatalog.SecretKey)
		if saveErr := s.saveSetting(settingcatalog.SecretKey, secret); saveErr != nil {
			logger.Warning("save secret failed:", saveErr)
			return []byte(secret), saveErr
		}
		return []byte(secret), nil
	}
	if err != nil {
		return nil, err
	}
	return []byte(setting.Value), nil
}

func (s *SettingService) GetInstallSalt() ([]byte, error) {
	setting, err := s.getSetting(settingcatalog.InstallSaltKey)
	if settingNotFound(err) {
		salt, _ := defaultSettingValue(settingcatalog.InstallSaltKey)
		if saveErr := s.saveSetting(settingcatalog.InstallSaltKey, salt); saveErr != nil {
			logger.Warning("save install salt failed:", saveErr)
			return []byte(salt), saveErr
		}
		return []byte(salt), nil
	}
	if err != nil {
		return nil, err
	}
	return []byte(setting.Value), nil
}

func (s *SettingService) GetSessionMaxAge() (int, error) {
	return s.getInt(settingcatalog.SessionMaxAgeKey)
}

func (s *SettingService) GetForceCookieSecure() (bool, error) {
	if enabled, ok, err := configsecurity.GetForceCookieSecureEnv(); ok {
		if err != nil {
			return false, common.NewError("invalid SUI_FORCE_COOKIE_SECURE")
		}
		return enabled, nil
	}
	return s.getBool(settingcatalog.ForceCookieSecureKey)
}

func (s *SettingService) GetSessionSameSiteStrict() (bool, error) {
	return s.getBool(settingcatalog.SessionSameSiteStrictKey)
}

func (s *SettingService) GetSessionGeneration() (string, error) {
	return s.getString(settingcatalog.SessionGenerationKey)
}

func (s *SettingService) RotateSessionGeneration() (string, error) {
	generation := common.Random(32)
	if err := s.setString(settingcatalog.SessionGenerationKey, generation); err != nil {
		return generation, err
	}
	realtime.CloseAll("session_rotated")
	invalidated := invalidateWSTokensForSessionRotation()
	if err := (&AuditService{}).Record(AuditEvent{
		Actor:    "system",
		Event:    "ws_tokens_invalidated",
		Resource: "realtime",
		Severity: AuditSeverityInfo,
		Details: map[string]any{
			"count": invalidated,
		},
	}); err != nil {
		logger.Warning("ws token invalidation audit failed:", err)
	}
	return generation, nil
}
