package catalog

const (
	WebListenKey   = "webListen"
	WebDomainKey   = "webDomain"
	WebPortKey     = "webPort"
	WebCertFileKey = "webCertFile"
	WebKeyFileKey  = "webKeyFile"
	WebPathKey     = "webPath"
	WebURIKey      = "webURI"

	SecretKey                = "secret"
	InstallSaltKey           = "installSalt"
	SessionMaxAgeKey         = "sessionMaxAge"
	ForceCookieSecureKey     = "forceCookieSecure"
	SessionSameSiteStrictKey = "sessionSameSiteStrict"
	SessionGenerationKey     = "sessionGeneration"
	SessionLifetimePolicyKey = "sessionLifetimePolicy"
	CoreSchemaVersionKey     = "coreSchemaVersion"

	TrafficAgeKey               = "trafficAge"
	TimeLocationKey             = "timeLocation"
	AuditRetentionDaysKey       = "auditRetentionDays"
	IPShowRawKey                = "ipShowRaw"
	IPHistoryRetentionDaysKey   = "ipHistoryRetentionDays"
	ObservabilityMemoryCapMBKey = "observabilityMemoryCapMB"
	UpdateChannelKey            = "updateChannel"

	ConfigKey  = "config"
	VersionKey = "version"
)

func WebDefaults() map[string]string {
	return map[string]string{
		WebListenKey:   "",
		WebDomainKey:   "",
		WebPortKey:     "2095",
		WebCertFileKey: "",
		WebKeyFileKey:  "",
		WebPathKey:     "/app/",
		WebURIKey:      "",
	}
}

func SessionDefaults(secret string, installSalt string) map[string]string {
	return map[string]string{
		SecretKey:                secret,
		InstallSaltKey:           installSalt,
		SessionMaxAgeKey:         "0",
		ForceCookieSecureKey:     "false",
		SessionSameSiteStrictKey: "false",
		SessionGenerationKey:     "",
		// Missing markers belong to installations that predate the bounded
		// session policy. Fresh installations persist bounded_v1 explicitly
		// while creating their bootstrap administrator.
		SessionLifetimePolicyKey: "legacy_unbounded",
		CoreSchemaVersionKey:     "1.8",
	}
}

func RuntimeDefaults() map[string]string {
	return map[string]string{
		TrafficAgeKey:               "30",
		TimeLocationKey:             "Europe/Moscow",
		AuditRetentionDaysKey:       "30",
		IPShowRawKey:                "false",
		IPHistoryRetentionDaysKey:   "30",
		ObservabilityMemoryCapMBKey: "32",
		UpdateChannelKey:            "main",
	}
}

func InternalDefaults(baseConfig string) map[string]string {
	return map[string]string{
		ConfigKey:  baseConfig,
		VersionKey: "",
	}
}
