package service

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func validatePaidSubSettingInput(key string, value string) error {
	if _, ok := paidSubBooleanSettingKeys[key]; ok {
		if _, err := strconv.ParseBool(value); err != nil {
			return common.NewError("invalid boolean setting: ", key)
		}
		return nil
	}
	switch key {
	case settingKeyPaidSubBotPollSeconds:
		if err := validateIntRange(key, value, 1, 50); err != nil {
			return err
		}
	case settingKeyPaidSubTrialDays:
		if err := validateIntRange(key, value, 0, 3650); err != nil {
			return err
		}
	case settingKeyPaidSubTrialVolumeGB:
		if err := validateIntRange(key, value, 0, 1048576); err != nil {
			return err
		}
	case settingKeyPaidSubMaxClients:
		if err := validateIntRange(key, value, 0, 10000000); err != nil {
			return err
		}
	case settingKeyPaidSubStartRateLimitPerMin:
		if err := validateIntRange(key, value, 0, 1000); err != nil {
			return err
		}
	case settingKeyPaidSubOrderTTLMinutes:
		if err := validateIntRange(key, value, 1, 1440); err != nil {
			return err
		}
	case settingKeyPaidSubAutoInbounds:
		if value != "" {
			var ids []uint
			if err := json.Unmarshal([]byte(value), &ids); err != nil {
				return common.NewError("paidSubAutoInbounds must be a JSON array of inbound ids")
			}
		}
	case settingKeyPaidSubCurrency:
		v := strings.ToUpper(strings.TrimSpace(value))
		if len(v) != 3 {
			return common.NewError("paidSubCurrency must be a 3-letter code")
		}
	case settingKeyPaidSubExternalURLTemplate:
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
	case settingKeyPaidSubTransportMode:
		if err := validateTransportMode(value); err != nil {
			return err
		}
	case settingKeyPaidSubOutboundTag:
		if len(value) > 256 {
			return common.NewError("paidSubOutboundTag is too long")
		}
	case settingKeyPaidSubGreeting:
		if len([]rune(value)) > 4096 {
			return common.NewError("paidSubGreeting is too long (max 4096)")
		}
	}
	return nil
}

func validateIntRange(key string, value string, min int, max int) error {
	n, err := strconv.Atoi(value)
	if err != nil || n < min || n > max {
		return common.NewErrorf("invalid setting %s: must be an integer in [%d, %d]", key, min, max)
	}
	return nil
}
