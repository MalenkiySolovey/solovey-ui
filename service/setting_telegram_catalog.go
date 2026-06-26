package service

import settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"

const (
	settingKeyTelegramEnabled             = settingcatalog.TelegramEnabledKey
	settingKeyTelegramBotToken            = settingcatalog.TelegramBotTokenKey
	settingKeyTelegramChatID              = settingcatalog.TelegramChatIDKey
	settingKeyTelegramProxyURL            = settingcatalog.TelegramProxyURLKey
	settingKeyTelegramProxyUsername       = settingcatalog.TelegramProxyUsernameKey
	settingKeyTelegramProxyPassword       = settingcatalog.TelegramProxyPasswordKey
	settingKeyTelegramTransportMode       = settingcatalog.TelegramTransportModeKey
	settingKeyTelegramOutboundTag         = settingcatalog.TelegramOutboundTagKey
	settingKeyTelegramCPUThreshold        = settingcatalog.TelegramCPUThresholdKey
	settingKeyTelegramNotifyCPU           = settingcatalog.TelegramNotifyCPUKey
	settingKeyTelegramReport              = settingcatalog.TelegramReportKey
	settingKeyTelegramReportCron          = settingcatalog.TelegramReportCronKey
	settingKeyTelegramBackupEnabled       = settingcatalog.TelegramBackupEnabledKey
	settingKeyTelegramBackupPassphrase    = settingcatalog.TelegramBackupPassphraseKey
	settingKeyTelegramBackupCron          = settingcatalog.TelegramBackupCronKey
	settingKeyTelegramBackupExcludeTables = settingcatalog.TelegramBackupExcludeTablesKey
	settingKeyTelegramBackupMaxSizeMB     = settingcatalog.TelegramBackupMaxSizeMBKey
)

var defaultTelegramSettingValues = settingcatalog.TelegramDefaults()

var telegramEncryptedSettingKeys = settingcatalog.TelegramEncryptedKeys()
