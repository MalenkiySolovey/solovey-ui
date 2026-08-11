//go:build !minimal

package telegram

import (
	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

const (
	telegramSettingsPage = "telegram"
	telegramCoreGroup    = "telegram_core"
	telegramBackupGroup  = "telegram_backup"
)

func registerSettingContribution() func() {
	return service.RegisterSettingContribution(id, service.SettingContribution{
		Defaults:                telegramsettings.Defaults(),
		Encrypted:               telegramsettings.EncryptedKeys(),
		ClearableEmptyEncrypted: telegramsettings.ClearableEmptyEncryptedKeys(),
		ImportAliases: map[string]string{
			"tgBotEnable": telegramsettings.EnabledKey,
			"tgBotToken":  telegramsettings.BotTokenKey,
			"tgBotChatId": telegramsettings.ChatIDKey,
			"tgRunTime":   telegramsettings.ReportCronKey,
			"tgCpu":       telegramsettings.CPUThresholdKey,
			"tgBotBackup": telegramsettings.BackupEnabledKey,
			"tgBotProxy":  telegramsettings.ProxyURLKey,
		},
		Fields: telegramSettingFields(),
		Validators: []service.SettingValidator{
			telegramsettings.Validate,
		},
	})
}

func telegramSettingFields() []settingsschema.Field {
	return []settingsschema.Field{
		{Key: telegramsettings.EnabledKey, Page: telegramSettingsPage, Group: telegramCoreGroup, Type: settingsschema.FieldTypeBool, LabelKey: "telegram.enabled", Order: 10},
		{Key: telegramsettings.BotTokenKey, Page: telegramSettingsPage, Group: telegramCoreGroup, Type: settingsschema.FieldTypeSecret, LabelKey: "telegram.botToken", Order: 20},
		{Key: telegramsettings.ChatIDKey, Page: telegramSettingsPage, Group: telegramCoreGroup, Type: settingsschema.FieldTypeString, LabelKey: "telegram.chatId", Order: 30},
		{Key: telegramsettings.CPUThresholdKey, Page: telegramSettingsPage, Group: telegramCoreGroup, Type: settingsschema.FieldTypeInt, LabelKey: "telegram.cpuThreshold", Min: intPtr(1), Max: intPtr(100), Order: 40},
		{Key: telegramsettings.NotifyCPUKey, Page: telegramSettingsPage, Group: telegramCoreGroup, Type: settingsschema.FieldTypeBool, LabelKey: "telegram.notifyCpu", Order: 50},
		{Key: telegramsettings.ReportKey, Page: telegramSettingsPage, Group: telegramCoreGroup, Type: settingsschema.FieldTypeBool, LabelKey: "telegram.report", Order: 60},
		{Key: telegramsettings.ReportCronKey, Page: telegramSettingsPage, Group: telegramCoreGroup, Type: settingsschema.FieldTypeCron, LabelKey: "telegram.reportCron", Order: 70},
		{Key: telegramsettings.TransportModeKey, Page: telegramSettingsPage, Group: telegramCoreGroup, Type: settingsschema.FieldTypeEnum, LabelKey: "telegram.transport", Options: []string{"proxy", "outbound"}, Order: 80},
		{Key: telegramsettings.OutboundTagKey, Page: telegramSettingsPage, Group: telegramCoreGroup, Type: settingsschema.FieldTypeString, LabelKey: "telegram.outbound", Order: 90},
		{Key: telegramsettings.ProxyURLKey, Page: telegramSettingsPage, Group: telegramCoreGroup, Type: settingsschema.FieldTypeSecret, LabelKey: "telegram.proxyUrl", Advanced: true, Order: 100},
		{Key: telegramsettings.ProxyUsernameKey, Page: telegramSettingsPage, Group: telegramCoreGroup, Type: settingsschema.FieldTypeSecret, LabelKey: "telegram.proxyUsername", Advanced: true, Order: 110},
		{Key: telegramsettings.ProxyPasswordKey, Page: telegramSettingsPage, Group: telegramCoreGroup, Type: settingsschema.FieldTypeSecret, LabelKey: "telegram.proxyPassword", Advanced: true, Order: 120},

		{Key: telegramsettings.BackupEnabledKey, Page: telegramSettingsPage, Group: telegramBackupGroup, Type: settingsschema.FieldTypeBool, LabelKey: "telegram.backup.enabled", Order: 10},
		{Key: telegramsettings.BackupPassphraseKey, Page: telegramSettingsPage, Group: telegramBackupGroup, Type: settingsschema.FieldTypeSecret, LabelKey: "telegram.backup.passphrase", Order: 20},
		{Key: telegramsettings.BackupCronKey, Page: telegramSettingsPage, Group: telegramBackupGroup, Type: settingsschema.FieldTypeCron, LabelKey: "telegram.backup.schedule.title", Order: 30},
		{Key: telegramsettings.BackupExcludeTablesKey, Page: telegramSettingsPage, Group: telegramBackupGroup, Type: settingsschema.FieldTypeTagList, LabelKey: "telegram.backup.excludeTables", Options: []string{"stats", "client_ips", "audit_events", "changes"}, Order: 40},
		{Key: telegramsettings.BackupMaxSizeMBKey, Page: telegramSettingsPage, Group: telegramBackupGroup, Type: settingsschema.FieldTypeInt, LabelKey: "telegram.backup.maxSize", Min: intPtr(1), Max: intPtr(50), Order: 50},
	}
}

func intPtr(value int) *int {
	return &value
}
