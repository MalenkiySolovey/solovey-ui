package service

import "github.com/MalenkiySolovey/solovey-ui/util/common"

var defaultWebSettingValues = map[string]string{
	"webListen":   "",
	"webDomain":   "",
	"webPort":     "2095",
	"webCertFile": "",
	"webKeyFile":  "",
	"webPath":     "/app/",
	"webURI":      "",
}

var defaultSessionSettingValues = map[string]string{
	"secret":                common.Random(32),
	"installSalt":           common.Random(32),
	"sessionMaxAge":         "0",
	"forceCookieSecure":     "false",
	"sessionSameSiteStrict": "false",
	"sessionGeneration":     "",
}

var defaultRuntimeSettingValues = map[string]string{
	"trafficAge":               "30",
	"timeLocation":             "Europe/Moscow",
	"auditRetentionDays":       "30",
	"ipShowRaw":                "false",
	"ipHistoryRetentionDays":   "30",
	"observabilityMemoryCapMB": "32",
}

var defaultInternalSettingValues = map[string]string{
	"config":  defaultSingBoxBaseConfig,
	"version": "",
}
