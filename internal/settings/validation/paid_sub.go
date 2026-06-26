package validation

import (
	"encoding/json"
	"strconv"
	"strings"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func ValidatePaidSubSettingInput(key string, value string) error {
	if _, ok := settingcatalog.PaidSubBooleanKeys()[key]; ok {
		if _, err := strconv.ParseBool(value); err != nil {
			return common.NewError("invalid boolean setting: ", key)
		}
		return nil
	}
	switch key {
	case settingcatalog.PaidSubBotPollSecondsKey:
		if err := ValidateIntRange(key, value, 1, 50); err != nil {
			return err
		}
	case settingcatalog.PaidSubTrialDaysKey:
		if err := ValidateIntRange(key, value, 0, 3650); err != nil {
			return err
		}
	case settingcatalog.PaidSubTrialVolumeGBKey:
		if err := ValidateIntRange(key, value, 0, 1048576); err != nil {
			return err
		}
	case settingcatalog.PaidSubMaxClientsKey:
		if err := ValidateIntRange(key, value, 0, 10000000); err != nil {
			return err
		}
	case settingcatalog.PaidSubStartRateLimitPerMinKey:
		if err := ValidateIntRange(key, value, 0, 1000); err != nil {
			return err
		}
	case settingcatalog.PaidSubOrderTTLMinutesKey:
		if err := ValidateIntRange(key, value, 1, 1440); err != nil {
			return err
		}
	case settingcatalog.PaidSubAutoInboundsKey:
		if value != "" {
			var ids []uint
			if err := json.Unmarshal([]byte(value), &ids); err != nil {
				return common.NewError("paidSubAutoInbounds must be a JSON array of inbound ids")
			}
		}
	case settingcatalog.PaidSubCurrencyKey:
		v := strings.ToUpper(strings.TrimSpace(value))
		if len(v) != 3 {
			return common.NewError("paidSubCurrency must be a 3-letter code")
		}
	case settingcatalog.PaidSubExternalURLTemplateKey:
		if value != "" {
			if len(value) > 2048 {
				return common.NewError("paidSubExternalUrlTemplate is too long")
			}
			if !strings.HasPrefix(value, "https://") {
				return common.NewError("paidSubExternalUrlTemplate must start with https://")
			}
			if strings.ContainsAny(value, " \t\r\n#") {
				return common.NewError("paidSubExternalUrlTemplate must not contain spaces or a fragment")
			}
		}
	case settingcatalog.PaidSubTransportModeKey:
		if err := ValidateTransportMode(value); err != nil {
			return err
		}
	case settingcatalog.PaidSubOutboundTagKey:
		if len(value) > 256 {
			return common.NewError("paidSubOutboundTag is too long")
		}
	case settingcatalog.PaidSubGreetingKey:
		if len([]rune(value)) > 4096 {
			return common.NewError("paidSubGreeting is too long (max 4096)")
		}
	}
	return nil
}
