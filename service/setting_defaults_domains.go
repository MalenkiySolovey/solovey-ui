package service

import (
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

var defaultWebSettingValues = settingcatalog.WebDefaults()

var defaultSessionSettingValues = settingcatalog.SessionDefaults(common.Random(32), common.Random(32))

var defaultRuntimeSettingValues = settingcatalog.RuntimeDefaults()

var defaultInternalSettingValues = settingcatalog.InternalDefaults(defaultSingBoxBaseConfig)
