package schema

import settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"

func webFieldMetadata() []Field {
	return []Field{
		{Key: settingcatalog.WebListenKey, Page: PageSettings, Group: GroupInterface, Type: FieldTypeString, LabelKey: "setting.addr", Order: 10},
		{Key: settingcatalog.WebPortKey, Page: PageSettings, Group: GroupInterface, Type: FieldTypeInt, LabelKey: "setting.port", Min: intPtr(1), Max: intPtr(65535), Order: 20},
		{Key: settingcatalog.WebPathKey, Page: PageSettings, Group: GroupInterface, Type: FieldTypePath, LabelKey: "setting.webPath", RestartRequired: true, Order: 30},
		{Key: settingcatalog.WebDomainKey, Page: PageSettings, Group: GroupInterface, Type: FieldTypeString, LabelKey: "setting.domain", Order: 40},
		{Key: settingcatalog.WebKeyFileKey, Page: PageSettings, Group: GroupInterface, Type: FieldTypePath, LabelKey: "setting.sslKey", Order: 50},
		{Key: settingcatalog.WebCertFileKey, Page: PageSettings, Group: GroupInterface, Type: FieldTypePath, LabelKey: "setting.sslCert", Order: 60},
		{Key: settingcatalog.WebURIKey, Page: PageSettings, Group: GroupInterface, Type: FieldTypeURL, LabelKey: "setting.webUri", Advanced: true, Order: 70},
	}
}

func sessionFieldMetadata() []Field {
	return []Field{
		{Key: settingcatalog.SecretKey, Page: PageInternal, Group: GroupInternal, Type: FieldTypeSecret, LabelKey: "setting.secret", Order: 10},
		{Key: settingcatalog.InstallSaltKey, Page: PageInternal, Group: GroupInternal, Type: FieldTypeSecret, LabelKey: "setting.installSalt", Order: 20},
		{Key: settingcatalog.SessionMaxAgeKey, Page: PageSettings, Group: GroupSession, Type: FieldTypeInt, LabelKey: "setting.sessionAge", Min: intPtr(0), Order: 30},
		{Key: settingcatalog.ForceCookieSecureKey, Page: PageSettings, Group: GroupSession, Type: FieldTypeBool, LabelKey: "setting.forceCookieSecure", Advanced: true, Order: 40},
		{Key: settingcatalog.SessionSameSiteStrictKey, Page: PageSettings, Group: GroupSession, Type: FieldTypeBool, LabelKey: "setting.sessionSameSiteStrict", Advanced: true, Order: 50},
		{Key: settingcatalog.SessionGenerationKey, Page: PageInternal, Group: GroupInternal, Type: FieldTypeString, LabelKey: "setting.sessionGeneration", Order: 60},
	}
}

func runtimeFieldMetadata() []Field {
	return []Field{
		{Key: settingcatalog.TrafficAgeKey, Page: PageSettings, Group: GroupRuntime, Type: FieldTypeInt, LabelKey: "setting.trafficAge", Min: intPtr(0), Order: 10},
		{Key: settingcatalog.TimeLocationKey, Page: PageSettings, Group: GroupRuntime, Type: FieldTypeString, LabelKey: "setting.timeLoc", Order: 20},
		{Key: settingcatalog.AuditRetentionDaysKey, Page: PageSettings, Group: GroupRuntime, Type: FieldTypeInt, LabelKey: "setting.auditRetentionDays", Min: intPtr(0), Advanced: true, Order: 30},
		{Key: settingcatalog.IPShowRawKey, Page: PageSettings, Group: GroupRuntime, Type: FieldTypeBool, LabelKey: "setting.ipShowRaw", Advanced: true, Order: 40},
		{Key: settingcatalog.IPHistoryRetentionDaysKey, Page: PageSettings, Group: GroupRuntime, Type: FieldTypeInt, LabelKey: "setting.ipHistoryRetentionDays", Min: intPtr(0), Advanced: true, Order: 50},
		{Key: settingcatalog.ObservabilityMemoryCapMBKey, Page: PageSettings, Group: GroupRuntime, Type: FieldTypeInt, LabelKey: "setting.observabilityMemoryCapMB", Min: intPtr(1), Advanced: true, Order: 60},
		{Key: settingcatalog.UpdateChannelKey, Page: PageSettings, Group: GroupRuntime, Type: FieldTypeEnum, LabelKey: "update.title", Options: []string{"main", "beta"}, Order: 70},
	}
}

func internalFieldMetadata() []Field {
	return []Field{
		{Key: settingcatalog.ConfigKey, Page: PageInternal, Group: GroupInternal, Type: FieldTypeJSON, LabelKey: "setting.config", Order: 10},
		{Key: settingcatalog.VersionKey, Page: PageInternal, Group: GroupInternal, Type: FieldTypeString, LabelKey: "setting.version", Order: 20},
	}
}
