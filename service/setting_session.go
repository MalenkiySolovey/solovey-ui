package service

import (
	"github.com/MalenkiySolovey/solovey-ui/config"
	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/realtime"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func (s *SettingService) GetSecret() ([]byte, error) {
	setting, err := s.getSetting("secret")
	if database.IsNotFound(err) {
		secret, _ := defaultSettingValue("secret")
		if saveErr := s.saveSetting("secret", secret); saveErr != nil {
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
	setting, err := s.getSetting("installSalt")
	if database.IsNotFound(err) {
		salt, _ := defaultSettingValue("installSalt")
		if saveErr := s.saveSetting("installSalt", salt); saveErr != nil {
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
	return s.getInt("sessionMaxAge")
}

func (s *SettingService) GetForceCookieSecure() (bool, error) {
	if enabled, ok, err := config.GetForceCookieSecureEnv(); ok {
		if err != nil {
			return false, common.NewError("invalid SUI_FORCE_COOKIE_SECURE")
		}
		return enabled, nil
	}
	return s.getBool("forceCookieSecure")
}

func (s *SettingService) GetSessionSameSiteStrict() (bool, error) {
	return s.getBool("sessionSameSiteStrict")
}

func (s *SettingService) GetSessionGeneration() (string, error) {
	return s.getString("sessionGeneration")
}

func (s *SettingService) RotateSessionGeneration() (string, error) {
	generation := common.Random(32)
	if err := s.setString("sessionGeneration", generation); err != nil {
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
