//go:build !minimal

package settings

import (
	"strconv"

	telegramschedule "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/schedule"
	settingsvalidation "github.com/MalenkiySolovey/solovey-ui/internal/settings/validation"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

// Validate applies the component-owned setting contract. It is shared by the
// runtime contribution and component tests so the accepted values cannot
// diverge between those paths.
func Validate(key string, value string, storedSecretMarker string) error {
	if _, ok := BooleanKeys()[key]; ok {
		if _, err := strconv.ParseBool(value); err != nil {
			return common.NewError("invalid boolean setting: ", key)
		}
		return nil
	}
	switch key {
	case CPUThresholdKey:
		threshold, err := strconv.Atoi(value)
		if err != nil || threshold <= 0 || threshold > 100 {
			return common.NewError("invalid cpu threshold setting")
		}
	case ReportCronKey, BackupCronKey:
		if _, err := telegramschedule.Parse(value); err != nil {
			return err
		}
	case BackupPassphraseKey:
		if value != "" && value != storedSecretMarker && len([]rune(value)) < 12 {
			return common.NewError("weak_passphrase")
		}
	case BackupExcludeTablesKey:
		if len(value) > 256 {
			return common.NewError("telegramBackupExcludeTables is too long")
		}
	case BackupMaxSizeMBKey:
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 50 {
			return common.NewError("invalid telegram backup max size setting")
		}
	case TransportModeKey:
		if err := settingsvalidation.ValidateTransportMode(value); err != nil {
			return err
		}
	case OutboundTagKey:
		if len(value) > 256 {
			return common.NewError("telegramOutboundTag is too long")
		}
	case ProxyURLKey:
		if err := settingsvalidation.ValidateProxyURLValue(value, storedSecretMarker); err != nil {
			return err
		}
	}
	return nil
}
