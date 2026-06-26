package validation

import (
	"strconv"
	"strings"
	"time"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/robfig/cron/v3"
)

var telegramReportCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func ParseTelegramReportCron(spec string) (cron.Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	schedule, err := telegramReportCronParser.Parse(spec)
	if err != nil {
		return nil, err
	}
	first := schedule.Next(time.Unix(0, 0))
	second := schedule.Next(first)
	if !second.IsZero() && second.Sub(first) < time.Minute {
		return nil, common.NewError("telegramReportCron step must be at least 1 minute")
	}
	return schedule, nil
}

func ValidateTelegramSettingInput(key string, value string, storedSecretMarker string) error {
	if _, ok := settingcatalog.TelegramBooleanKeys()[key]; ok {
		if _, err := strconv.ParseBool(value); err != nil {
			return common.NewError("invalid boolean setting: ", key)
		}
		return nil
	}
	switch key {
	case settingcatalog.TelegramBackupEnabledKey:
		if value != "true" && value != "false" {
			return common.NewError("invalid boolean setting: ", key)
		}
	case settingcatalog.TelegramCPUThresholdKey:
		threshold, err := strconv.Atoi(value)
		if err != nil || threshold <= 0 || threshold > 100 {
			return common.NewError("invalid cpu threshold setting")
		}
	case settingcatalog.TelegramReportCronKey:
		if _, err := ParseTelegramReportCron(value); err != nil {
			return err
		}
	case settingcatalog.TelegramBackupPassphraseKey:
		if value != "" && value != storedSecretMarker && len([]rune(value)) < 12 {
			return common.NewError("weak_passphrase")
		}
	case settingcatalog.TelegramBackupCronKey:
		if _, err := ParseTelegramReportCron(value); err != nil {
			return err
		}
	case settingcatalog.TelegramBackupExcludeTablesKey:
		if len(value) > 256 {
			return common.NewError("telegramBackupExcludeTables is too long")
		}
	case settingcatalog.TelegramBackupMaxSizeMBKey:
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 50 {
			return common.NewError("invalid telegram backup max size setting")
		}
	case settingcatalog.TelegramTransportModeKey:
		if err := ValidateTransportMode(value); err != nil {
			return err
		}
	case settingcatalog.TelegramOutboundTagKey:
		if len(value) > 256 {
			return common.NewError("telegramOutboundTag is too long")
		}
	}
	return nil
}
