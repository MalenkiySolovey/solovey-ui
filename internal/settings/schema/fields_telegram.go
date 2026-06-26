package schema

import settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"

func telegramFieldMetadata() []Field {
	return []Field{
		{Key: settingcatalog.TelegramEnabledKey, Page: PageTelegram, Group: GroupTelegramCore, Type: FieldTypeBool, LabelKey: "telegram.enabled", Order: 10},
		{Key: settingcatalog.TelegramBotTokenKey, Page: PageTelegram, Group: GroupTelegramCore, Type: FieldTypeSecret, LabelKey: "telegram.botToken", Order: 20},
		{Key: settingcatalog.TelegramChatIDKey, Page: PageTelegram, Group: GroupTelegramCore, Type: FieldTypeString, LabelKey: "telegram.chatId", Order: 30},
		{Key: settingcatalog.TelegramCPUThresholdKey, Page: PageTelegram, Group: GroupTelegramCore, Type: FieldTypeInt, LabelKey: "telegram.cpuThreshold", Min: intPtr(1), Max: intPtr(100), Order: 40},
		{Key: settingcatalog.TelegramNotifyCPUKey, Page: PageTelegram, Group: GroupTelegramCore, Type: FieldTypeBool, LabelKey: "telegram.notifyCpu", Order: 50},
		{Key: settingcatalog.TelegramReportKey, Page: PageTelegram, Group: GroupTelegramCore, Type: FieldTypeBool, LabelKey: "telegram.report", Order: 60},
		{Key: settingcatalog.TelegramReportCronKey, Page: PageTelegram, Group: GroupTelegramCore, Type: FieldTypeCron, LabelKey: "telegram.reportCron", Order: 70},
		{Key: settingcatalog.TelegramTransportModeKey, Page: PageTelegram, Group: GroupTelegramCore, Type: FieldTypeEnum, LabelKey: "telegram.transport", Options: []string{"proxy", "outbound"}, Order: 80},
		{Key: settingcatalog.TelegramOutboundTagKey, Page: PageTelegram, Group: GroupTelegramCore, Type: FieldTypeString, LabelKey: "telegram.outbound", Order: 90},
		{Key: settingcatalog.TelegramProxyURLKey, Page: PageTelegram, Group: GroupTelegramCore, Type: FieldTypeSecret, LabelKey: "telegram.proxyUrl", Advanced: true, Order: 100},
		{Key: settingcatalog.TelegramProxyUsernameKey, Page: PageTelegram, Group: GroupTelegramCore, Type: FieldTypeSecret, LabelKey: "telegram.proxyUsername", Advanced: true, Order: 110},
		{Key: settingcatalog.TelegramProxyPasswordKey, Page: PageTelegram, Group: GroupTelegramCore, Type: FieldTypeSecret, LabelKey: "telegram.proxyPassword", Advanced: true, Order: 120},

		{Key: settingcatalog.TelegramBackupEnabledKey, Page: PageTelegram, Group: GroupTelegramBackup, Type: FieldTypeBool, LabelKey: "telegram.backup.enabled", Order: 10},
		{Key: settingcatalog.TelegramBackupPassphraseKey, Page: PageTelegram, Group: GroupTelegramBackup, Type: FieldTypeSecret, LabelKey: "telegram.backup.passphrase", Order: 20},
		{Key: settingcatalog.TelegramBackupCronKey, Page: PageTelegram, Group: GroupTelegramBackup, Type: FieldTypeCron, LabelKey: "telegram.backup.schedule.title", Order: 30},
		{Key: settingcatalog.TelegramBackupExcludeTablesKey, Page: PageTelegram, Group: GroupTelegramBackup, Type: FieldTypeTagList, LabelKey: "telegram.backup.excludeTables", Options: []string{"stats", "client_ips", "audit_events", "changes"}, Order: 40},
		{Key: settingcatalog.TelegramBackupMaxSizeMBKey, Page: PageTelegram, Group: GroupTelegramBackup, Type: FieldTypeInt, LabelKey: "telegram.backup.maxSize", Min: intPtr(1), Max: intPtr(50), Order: 50},
	}
}
