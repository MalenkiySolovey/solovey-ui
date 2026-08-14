package service

import (
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

var defaultWebSettingValues = settingcatalog.WebDefaults()

var defaultSessionSettingValues = settingcatalog.SessionDefaults(mustSecureSettingDefault(32), mustSecureSettingDefault(32))

func mustSecureSettingDefault(length int) string {
	value, err := common.SecureRandom(length)
	if err != nil {
		panic("secure setting defaults are unavailable: " + err.Error())
	}
	return value
}

var defaultRuntimeSettingValues = settingcatalog.RuntimeDefaults()

var defaultInternalSettingValues = settingcatalog.InternalDefaults(defaultSingBoxBaseConfig)
