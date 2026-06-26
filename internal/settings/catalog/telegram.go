package catalog

const (
	TelegramEnabledKey             = "telegramEnabled"
	TelegramBotTokenKey            = "telegramBotToken"
	TelegramChatIDKey              = "telegramChatID"
	TelegramProxyURLKey            = "telegramProxyURL"
	TelegramProxyUsernameKey       = "telegramProxyUsername"
	TelegramProxyPasswordKey       = "telegramProxyPassword"
	TelegramTransportModeKey       = "telegramTransportMode"
	TelegramOutboundTagKey         = "telegramOutboundTag"
	TelegramCPUThresholdKey        = "telegramCpuThreshold"
	TelegramNotifyCPUKey           = "telegramNotifyCpu"
	TelegramReportKey              = "telegramReport"
	TelegramReportCronKey          = "telegramReportCron"
	TelegramBackupEnabledKey       = "telegramBackupEnabled"
	TelegramBackupPassphraseKey    = "telegramBackupPassphrase"
	TelegramBackupCronKey          = "telegramBackupCron"
	TelegramBackupExcludeTablesKey = "telegramBackupExcludeTables"
	TelegramBackupMaxSizeMBKey     = "telegramBackupMaxSizeMB"
)

func TelegramDefaults() map[string]string {
	return map[string]string{
		TelegramEnabledKey:             "false",
		TelegramBotTokenKey:            "",
		TelegramChatIDKey:              "",
		TelegramProxyURLKey:            "",
		TelegramProxyUsernameKey:       "",
		TelegramProxyPasswordKey:       "",
		TelegramTransportModeKey:       "proxy",
		TelegramOutboundTagKey:         "",
		TelegramCPUThresholdKey:        "90",
		TelegramNotifyCPUKey:           "false",
		TelegramReportKey:              "false",
		TelegramReportCronKey:          "",
		TelegramBackupEnabledKey:       "false",
		TelegramBackupPassphraseKey:    "",
		TelegramBackupCronKey:          "",
		TelegramBackupExcludeTablesKey: "stats,client_ips,audit_events,changes",
		TelegramBackupMaxSizeMBKey:     "45",
	}
}

func TelegramBooleanKeys() map[string]struct{} {
	return KeySet(
		TelegramNotifyCPUKey,
		TelegramReportKey,
	)
}

func TelegramEncryptedKeys() map[string]struct{} {
	return KeySet(
		TelegramBackupPassphraseKey,
		TelegramBotTokenKey,
		TelegramProxyPasswordKey,
		TelegramProxyURLKey,
		TelegramProxyUsernameKey,
	)
}
