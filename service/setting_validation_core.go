package service

import (
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func validateSessionSettingInput(key string, value string) error {
	switch key {
	case "forceCookieSecure", "sessionSameSiteStrict":
		if _, err := strconv.ParseBool(value); err != nil {
			return common.NewError("invalid boolean setting: ", key)
		}
	}
	return nil
}

func validateRuntimeSettingInput(key string, value string) error {
	switch key {
	case "observabilityMemoryCapMB":
		capMB, err := strconv.Atoi(value)
		if err != nil || capMB <= 0 || capMB > 1024 {
			return common.NewError("invalid observability memory cap setting")
		}
	}
	return nil
}

func validateTransportMode(value string) error {
	switch value {
	case "proxy", "outbound":
		return nil
	default:
		return common.NewError("transport mode must be 'proxy' or 'outbound'")
	}
}
