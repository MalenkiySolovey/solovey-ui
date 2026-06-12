package service

const (
	settingKeyTelegramEnabled             = "telegramEnabled"
	settingKeyTelegramBotToken            = "telegramBotToken"
	settingKeyTelegramChatID              = "telegramChatID"
	settingKeyTelegramProxyURL            = "telegramProxyURL"
	settingKeyTelegramProxyUsername       = "telegramProxyUsername"
	settingKeyTelegramProxyPassword       = "telegramProxyPassword"
	settingKeyTelegramTransportMode       = "telegramTransportMode"
	settingKeyTelegramOutboundTag         = "telegramOutboundTag"
	settingKeyTelegramCPUThreshold        = "telegramCpuThreshold"
	settingKeyTelegramNotifyCPU           = "telegramNotifyCpu"
	settingKeyTelegramReport              = "telegramReport"
	settingKeyTelegramReportCron          = "telegramReportCron"
	settingKeyTelegramBackupEnabled       = "telegramBackupEnabled"
	settingKeyTelegramBackupPassphrase    = "telegramBackupPassphrase"
	settingKeyTelegramBackupCron          = "telegramBackupCron"
	settingKeyTelegramBackupExcludeTables = "telegramBackupExcludeTables"
	settingKeyTelegramBackupMaxSizeMB     = "telegramBackupMaxSizeMB"
)

var defaultTelegramSettingValues = map[string]string{
	settingKeyTelegramEnabled:             "false",
	settingKeyTelegramBotToken:            "",
	settingKeyTelegramChatID:              "",
	settingKeyTelegramProxyURL:            "",
	settingKeyTelegramProxyUsername:       "",
	settingKeyTelegramProxyPassword:       "",
	settingKeyTelegramTransportMode:       "proxy",
	settingKeyTelegramOutboundTag:         "",
	settingKeyTelegramCPUThreshold:        "90",
	settingKeyTelegramNotifyCPU:           "false",
	settingKeyTelegramReport:              "false",
	settingKeyTelegramReportCron:          "",
	settingKeyTelegramBackupEnabled:       "false",
	settingKeyTelegramBackupPassphrase:    "",
	settingKeyTelegramBackupCron:          "",
	settingKeyTelegramBackupExcludeTables: "stats,client_ips,audit_events,changes",
	settingKeyTelegramBackupMaxSizeMB:     "45",
}

var telegramBooleanSettingKeys = settingKeySet(
	settingKeyTelegramNotifyCPU,
	settingKeyTelegramReport,
)

var telegramEncryptedSettingKeys = settingKeySet(
	settingKeyTelegramBackupPassphrase,
	settingKeyTelegramBotToken,
	settingKeyTelegramProxyPassword,
	settingKeyTelegramProxyURL,
	settingKeyTelegramProxyUsername,
)
