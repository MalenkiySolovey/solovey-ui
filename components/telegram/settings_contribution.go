//go:build !minimal

package telegram

import (
	"strconv"
	"strings"
	"time"

	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"
	settingsvalidation "github.com/MalenkiySolovey/solovey-ui/internal/settings/validation"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/robfig/cron/v3"
)

const (
	telegramSettingsPage = "telegram"
	telegramCoreGroup    = "telegram_core"
	telegramBackupGroup  = "telegram_backup"
)

var telegramCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func registerSettingContribution() func() {
	return service.RegisterSettingContribution(id, service.SettingContribution{
		Defaults:                telegramsettings.Defaults(),
		Encrypted:               telegramsettings.EncryptedKeys(),
		ClearableEmptyEncrypted: telegramsettings.ClearableEmptyEncryptedKeys(),
		Fields:                  telegramSettingFields(),
		Validators: []service.SettingValidator{
			validateTelegramSettingInput,
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

func validateTelegramSettingInput(key string, value string, storedSecretMarker string) error {
	if _, ok := telegramsettings.BooleanKeys()[key]; ok {
		if _, err := strconv.ParseBool(value); err != nil {
			return common.NewError("invalid boolean setting: ", key)
		}
		return nil
	}
	switch key {
	case telegramsettings.BackupEnabledKey:
		if value != "true" && value != "false" {
			return common.NewError("invalid boolean setting: ", key)
		}
	case telegramsettings.CPUThresholdKey:
		threshold, err := strconv.Atoi(value)
		if err != nil || threshold <= 0 || threshold > 100 {
			return common.NewError("invalid cpu threshold setting")
		}
	case telegramsettings.ReportCronKey, telegramsettings.BackupCronKey:
		if _, err := parseTelegramCron(value); err != nil {
			return err
		}
	case telegramsettings.BackupPassphraseKey:
		if value != "" && value != storedSecretMarker && len([]rune(value)) < 12 {
			return common.NewError("weak_passphrase")
		}
	case telegramsettings.BackupExcludeTablesKey:
		if len(value) > 256 {
			return common.NewError("telegramBackupExcludeTables is too long")
		}
	case telegramsettings.BackupMaxSizeMBKey:
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 50 {
			return common.NewError("invalid telegram backup max size setting")
		}
	case telegramsettings.TransportModeKey:
		if err := settingsvalidation.ValidateTransportMode(value); err != nil {
			return err
		}
	case telegramsettings.OutboundTagKey:
		if len(value) > 256 {
			return common.NewError("telegramOutboundTag is too long")
		}
	case telegramsettings.ProxyURLKey:
		if err := settingsvalidation.ValidateProxyURLValue(value, storedSecretMarker); err != nil {
			return err
		}
	}
	return nil
}

func parseTelegramCron(spec string) (cron.Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	schedule, err := telegramCronParser.Parse(spec)
	if err != nil {
		return nil, err
	}
	first := schedule.Next(time.Unix(0, 0))
	second := schedule.Next(first)
	if !second.IsZero() && second.Sub(first) < time.Minute {
		return nil, common.NewError("telegram cron step must be at least 1 minute")
	}
	return schedule, nil
}

func intPtr(value int) *int {
	return &value
}
