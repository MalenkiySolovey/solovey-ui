package service

import (
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func validateTelegramSettingInput(key string, value string) error {
	if _, ok := telegramBooleanSettingKeys[key]; ok {
		if _, err := strconv.ParseBool(value); err != nil {
			return common.NewError("invalid boolean setting: ", key)
		}
		return nil
	}
	switch key {
	case settingKeyTelegramBackupEnabled:
		if value != "true" && value != "false" {
			return common.NewError("invalid boolean setting: ", key)
		}
	case settingKeyTelegramCPUThreshold:
		threshold, err := strconv.Atoi(value)
		if err != nil || threshold <= 0 || threshold > 100 {
			return common.NewError("invalid cpu threshold setting")
		}
	case settingKeyTelegramReportCron:
		if _, err := ParseTelegramReportCron(value); err != nil {
			return err
		}
	case settingKeyTelegramBackupPassphrase:
		if value != "" && value != StoredSecretMarker && len([]rune(value)) < 12 {
			return common.NewError("weak_passphrase")
		}
	case settingKeyTelegramBackupCron:
		if _, err := ParseTelegramReportCron(value); err != nil {
			return err
		}
	case settingKeyTelegramBackupExcludeTables:
		if len(value) > 256 {
			return common.NewError("telegramBackupExcludeTables is too long")
		}
	case settingKeyTelegramBackupMaxSizeMB:
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 50 {
			return common.NewError("invalid telegram backup max size setting")
		}
	case settingKeyTelegramTransportMode:
		if err := validateTransportMode(value); err != nil {
			return err
		}
	case settingKeyTelegramOutboundTag:
		if len(value) > 256 {
			return common.NewError("telegramOutboundTag is too long")
		}
	}
	return nil
}
