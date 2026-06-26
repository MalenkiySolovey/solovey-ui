package service

import (
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

func (s *SettingService) GetTelegramCpuThreshold() (int, error) {
	return s.getInt(settingKeyTelegramCPUThreshold)
}

func (s *SettingService) GetTelegramNotifyCpu() (bool, error) {
	return s.getBool(settingKeyTelegramNotifyCPU)
}

func (s *SettingService) GetTelegramEnabled() (bool, error) {
	return s.getBool(settingKeyTelegramEnabled)
}

func (s *SettingService) GetTelegramBotToken() (string, error) {
	return s.getString(settingKeyTelegramBotToken)
}

func (s *SettingService) GetTelegramChatID() (string, error) {
	return s.getString(settingKeyTelegramChatID)
}

func (s *SettingService) GetTelegramProxyURL() (string, error) {
	return s.getString(settingKeyTelegramProxyURL)
}

func (s *SettingService) GetTelegramProxyUsername() (string, error) {
	return s.getString(settingKeyTelegramProxyUsername)
}

func (s *SettingService) GetTelegramProxyPassword() (string, error) {
	return s.getString(settingKeyTelegramProxyPassword)
}

func (s *SettingService) GetTelegramTransportMode() (string, error) {
	return s.getString(settingKeyTelegramTransportMode)
}

func (s *SettingService) GetTelegramOutboundTag() (string, error) {
	return s.getString(settingKeyTelegramOutboundTag)
}

func (s *SettingService) GetTelegramReport() (bool, error) {
	return s.getBool(settingKeyTelegramReport)
}

func (s *SettingService) GetTelegramReportCron() (string, error) {
	return s.getString(settingKeyTelegramReportCron)
}

func (s *SettingService) GetTelegramBackupEnabled() (bool, error) {
	return s.getBool(settingKeyTelegramBackupEnabled)
}

func (s *SettingService) GetTelegramBackupCron() (string, error) {
	return s.getString(settingKeyTelegramBackupCron)
}

func (s *SettingService) GetTelegramBackupExcludeTables() (string, error) {
	return s.getString(settingKeyTelegramBackupExcludeTables)
}

func (s *SettingService) GetTelegramBackupMaxSizeMB() (int, error) {
	return s.getInt(settingKeyTelegramBackupMaxSizeMB)
}

func (s *SettingService) GetTelegramBackupPassphraseBytes() ([]byte, error) {
	setting, err := s.getSetting(settingKeyTelegramBackupPassphrase)
	if settingNotFound(err) {
		value, _ := defaultSettingValue(settingKeyTelegramBackupPassphrase)
		return []byte(value), nil
	}
	if err != nil {
		return nil, err
	}
	return s.decryptSettingBytes(settingKeyTelegramBackupPassphrase, setting.Value)
}

func (s *SettingService) HasTelegramBackupPassphrase() (bool, error) {
	setting, err := s.getSetting(settingKeyTelegramBackupPassphrase)
	if settingNotFound(err) {
		value, _ := defaultSettingValue(settingKeyTelegramBackupPassphrase)
		return value != "", nil
	}
	if err != nil {
		return false, err
	}
	return setting.Value != "", nil
}

func (s *SettingService) recordTelegramBackupPassphraseChanged(actor string, configured bool) {
	if !settingsDatabaseAvailable() {
		return
	}
	if err := (&AuditService{}).Record(AuditEvent{
		Actor:    actor,
		Event:    "tg_backup_passphrase_changed",
		Resource: "database",
		Severity: AuditSeverityInfo,
		Details: map[string]any{
			"configured": configured,
		},
	}); err != nil {
		logger.Warning("telegram backup passphrase audit failed:", err)
	}
}
