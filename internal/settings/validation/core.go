package validation

import (
	"strconv"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func ValidateSessionSettingInput(key string, value string) error {
	switch key {
	case settingcatalog.ForceCookieSecureKey, settingcatalog.SessionSameSiteStrictKey:
		if _, err := strconv.ParseBool(value); err != nil {
			return common.NewError("invalid boolean setting: ", key)
		}
	}
	return nil
}

func ValidateRuntimeSettingInput(key string, value string) error {
	switch key {
	case settingcatalog.ObservabilityMemoryCapMBKey:
		capMB, err := strconv.Atoi(value)
		if err != nil || capMB <= 0 || capMB > 1024 {
			return common.NewError("invalid observability memory cap setting")
		}
	}
	return nil
}
