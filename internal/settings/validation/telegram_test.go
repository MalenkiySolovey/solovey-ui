package validation

import (
	"strings"
	"testing"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
)

func TestParseTelegramReportCron(t *testing.T) {
	if _, err := ParseTelegramReportCron("*/5 * * * *"); err != nil {
		t.Fatalf("valid telegram cron rejected: %v", err)
	}
	if schedule, err := ParseTelegramReportCron(" "); err != nil || schedule != nil {
		t.Fatalf("empty telegram cron = %v, %v", schedule, err)
	}
	for _, spec := range []string{"* * * * * *", "@every 30s"} {
		if _, err := ParseTelegramReportCron(spec); err == nil {
			t.Fatalf("invalid telegram cron accepted: %q", spec)
		}
	}
}

func TestValidateTelegramSettingInput(t *testing.T) {
	const marker = "stored"
	valid := map[string]string{
		settingcatalog.TelegramNotifyCPUKey:           "true",
		settingcatalog.TelegramBackupEnabledKey:       "false",
		settingcatalog.TelegramCPUThresholdKey:        "90",
		settingcatalog.TelegramReportCronKey:          "*/10 * * * *",
		settingcatalog.TelegramBackupCronKey:          "",
		settingcatalog.TelegramBackupPassphraseKey:    "correct horse battery staple",
		settingcatalog.TelegramBackupExcludeTablesKey: "stats,client_ips",
		settingcatalog.TelegramBackupMaxSizeMBKey:     "45",
		settingcatalog.TelegramTransportModeKey:       "proxy",
		settingcatalog.TelegramOutboundTagKey:         strings.Repeat("a", 256),
	}
	for key, value := range valid {
		if err := ValidateTelegramSettingInput(key, value, marker); err != nil {
			t.Fatalf("valid telegram setting %s rejected: %v", key, err)
		}
	}
	if err := ValidateTelegramSettingInput(settingcatalog.TelegramBackupPassphraseKey, marker, marker); err != nil {
		t.Fatalf("stored marker rejected: %v", err)
	}

	invalid := map[string]string{
		settingcatalog.TelegramNotifyCPUKey:           "sometimes",
		settingcatalog.TelegramBackupEnabledKey:       "sometimes",
		settingcatalog.TelegramCPUThresholdKey:        "101",
		settingcatalog.TelegramReportCronKey:          "* * * * * *",
		settingcatalog.TelegramBackupCronKey:          "* * * * * *",
		settingcatalog.TelegramBackupPassphraseKey:    "short",
		settingcatalog.TelegramBackupExcludeTablesKey: strings.Repeat("a", 257),
		settingcatalog.TelegramBackupMaxSizeMBKey:     "51",
		settingcatalog.TelegramTransportModeKey:       "direct",
		settingcatalog.TelegramOutboundTagKey:         strings.Repeat("a", 257),
	}
	for key, value := range invalid {
		if err := ValidateTelegramSettingInput(key, value, marker); err == nil {
			t.Fatalf("invalid telegram setting %s accepted", key)
		}
	}
}
