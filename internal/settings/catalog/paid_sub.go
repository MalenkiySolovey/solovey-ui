package catalog

const (
	PaidSubEnabledKey              = "paidSubEnabled"
	PaidSubBotTokenKey             = "paidSubBotToken"
	PaidSubBotPollSecondsKey       = "paidSubBotPollSeconds"
	PaidSubUpdateOffsetKey         = "paidSubUpdateOffset"
	PaidSubTransportModeKey        = "paidSubTransportMode"
	PaidSubProxyURLKey             = "paidSubProxyURL"
	PaidSubProxyUsernameKey        = "paidSubProxyUsername"
	PaidSubProxyPasswordKey        = "paidSubProxyPassword"
	PaidSubOutboundTagKey          = "paidSubOutboundTag"
	PaidSubAutoRegisterKey         = "paidSubAutoRegister"
	PaidSubAutoInboundsKey         = "paidSubAutoInbounds"
	PaidSubTrialDaysKey            = "paidSubTrialDays"
	PaidSubTrialVolumeGBKey        = "paidSubTrialVolumeGB"
	PaidSubMaxClientsKey           = "paidSubMaxClients"
	PaidSubStartRateLimitPerMinKey = "paidSubStartRateLimitPerMin"
	PaidSubCurrencyKey             = "paidSubCurrency"
	PaidSubStarsEnabledKey         = "paidSubStarsEnabled"
	PaidSubYooKassaEnabledKey      = "paidSubYooKassaEnabled"
	PaidSubYooKassaTokenKey        = "paidSubYooKassaToken"
	PaidSubStripeEnabledKey        = "paidSubStripeEnabled"
	PaidSubStripeTokenKey          = "paidSubStripeToken"
	PaidSubPayMasterEnabledKey     = "paidSubPayMasterEnabled"
	PaidSubPayMasterTokenKey       = "paidSubPayMasterToken"
	PaidSubCryptoBotEnabledKey     = "paidSubCryptoBotEnabled"
	PaidSubCryptoBotTokenKey       = "paidSubCryptoBotToken"
	PaidSubExternalEnabledKey      = "paidSubExternalEnabled"
	PaidSubExternalURLTemplateKey  = "paidSubExternalUrlTemplate"
	PaidSubOrderTTLMinutesKey      = "paidSubOrderTTLMinutes"
	PaidSubGreetingKey             = "paidSubGreeting"
	PaidSubRefundRevokeKey         = "paidSubRefundRevoke"
)

func PaidSubDefaults() map[string]string {
	return map[string]string{
		PaidSubEnabledKey:              "false",
		PaidSubBotTokenKey:             "",
		PaidSubBotPollSecondsKey:       "25",
		PaidSubUpdateOffsetKey:         "0",
		PaidSubTransportModeKey:        "proxy",
		PaidSubProxyURLKey:             "",
		PaidSubProxyUsernameKey:        "",
		PaidSubProxyPasswordKey:        "",
		PaidSubOutboundTagKey:          "",
		PaidSubAutoRegisterKey:         "false",
		PaidSubAutoInboundsKey:         "[]",
		PaidSubTrialDaysKey:            "3",
		PaidSubTrialVolumeGBKey:        "0",
		PaidSubMaxClientsKey:           "5000",
		PaidSubStartRateLimitPerMinKey: "3",
		PaidSubCurrencyKey:             "RUB",
		PaidSubStarsEnabledKey:         "false",
		PaidSubYooKassaEnabledKey:      "false",
		PaidSubYooKassaTokenKey:        "",
		PaidSubStripeEnabledKey:        "false",
		PaidSubStripeTokenKey:          "",
		PaidSubPayMasterEnabledKey:     "false",
		PaidSubPayMasterTokenKey:       "",
		PaidSubCryptoBotEnabledKey:     "false",
		PaidSubCryptoBotTokenKey:       "",
		PaidSubExternalEnabledKey:      "false",
		PaidSubExternalURLTemplateKey:  "",
		PaidSubOrderTTLMinutesKey:      "30",
		PaidSubGreetingKey:             "",
		PaidSubRefundRevokeKey:         "true",
	}
}

func PaidSubBooleanKeys() map[string]struct{} {
	return KeySet(
		PaidSubEnabledKey,
		PaidSubAutoRegisterKey,
		PaidSubStarsEnabledKey,
		PaidSubYooKassaEnabledKey,
		PaidSubStripeEnabledKey,
		PaidSubPayMasterEnabledKey,
		PaidSubCryptoBotEnabledKey,
		PaidSubExternalEnabledKey,
		PaidSubRefundRevokeKey,
	)
}

func PaidSubEncryptedKeys() map[string]struct{} {
	return KeySet(
		PaidSubBotTokenKey,
		PaidSubYooKassaTokenKey,
		PaidSubStripeTokenKey,
		PaidSubPayMasterTokenKey,
		PaidSubCryptoBotTokenKey,
		PaidSubProxyURLKey,
		PaidSubProxyUsernameKey,
		PaidSubProxyPasswordKey,
	)
}
