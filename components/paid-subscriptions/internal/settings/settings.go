//go:build !minimal

// Package settings owns Paid Subscriptions component setting keys and defaults.
package settings

const (
	EnabledKey              = "paidSubEnabled"
	BotTokenKey             = "paidSubBotToken"
	BotPollSecondsKey       = "paidSubBotPollSeconds"
	UpdateOffsetKey         = "paidSubUpdateOffset"
	TransportModeKey        = "paidSubTransportMode"
	ProxyURLKey             = "paidSubProxyURL"
	ProxyUsernameKey        = "paidSubProxyUsername"
	ProxyPasswordKey        = "paidSubProxyPassword"
	OutboundTagKey          = "paidSubOutboundTag"
	AutoRegisterKey         = "paidSubAutoRegister"
	AutoInboundsKey         = "paidSubAutoInbounds"
	TrialDaysKey            = "paidSubTrialDays"
	TrialVolumeGBKey        = "paidSubTrialVolumeGB"
	MaxClientsKey           = "paidSubMaxClients"
	StartRateLimitPerMinKey = "paidSubStartRateLimitPerMin"
	CurrencyKey             = "paidSubCurrency"
	StarsEnabledKey         = "paidSubStarsEnabled"
	YooKassaEnabledKey      = "paidSubYooKassaEnabled"
	YooKassaTokenKey        = "paidSubYooKassaToken"
	StripeEnabledKey        = "paidSubStripeEnabled"
	StripeTokenKey          = "paidSubStripeToken"
	PayMasterEnabledKey     = "paidSubPayMasterEnabled"
	PayMasterTokenKey       = "paidSubPayMasterToken"
	CryptoBotEnabledKey     = "paidSubCryptoBotEnabled"
	CryptoBotTokenKey       = "paidSubCryptoBotToken"
	ExternalEnabledKey      = "paidSubExternalEnabled"
	ExternalURLTemplateKey  = "paidSubExternalUrlTemplate"
	OrderTTLMinutesKey      = "paidSubOrderTTLMinutes"
	GreetingKey             = "paidSubGreeting"
	RefundRevokeKey         = "paidSubRefundRevoke"
)

func Defaults() map[string]string {
	return map[string]string{
		EnabledKey:              "false",
		BotTokenKey:             "",
		BotPollSecondsKey:       "25",
		UpdateOffsetKey:         "0",
		TransportModeKey:        "proxy",
		ProxyURLKey:             "",
		ProxyUsernameKey:        "",
		ProxyPasswordKey:        "",
		OutboundTagKey:          "",
		AutoRegisterKey:         "false",
		AutoInboundsKey:         "[]",
		TrialDaysKey:            "3",
		TrialVolumeGBKey:        "0",
		MaxClientsKey:           "5000",
		StartRateLimitPerMinKey: "3",
		CurrencyKey:             "RUB",
		StarsEnabledKey:         "false",
		YooKassaEnabledKey:      "false",
		YooKassaTokenKey:        "",
		StripeEnabledKey:        "false",
		StripeTokenKey:          "",
		PayMasterEnabledKey:     "false",
		PayMasterTokenKey:       "",
		CryptoBotEnabledKey:     "false",
		CryptoBotTokenKey:       "",
		ExternalEnabledKey:      "false",
		ExternalURLTemplateKey:  "",
		OrderTTLMinutesKey:      "30",
		GreetingKey:             "",
		RefundRevokeKey:         "true",
	}
}

func BooleanKeys() map[string]struct{} {
	return keySet(
		EnabledKey,
		AutoRegisterKey,
		StarsEnabledKey,
		YooKassaEnabledKey,
		StripeEnabledKey,
		PayMasterEnabledKey,
		CryptoBotEnabledKey,
		ExternalEnabledKey,
		RefundRevokeKey,
	)
}

func EncryptedKeys() map[string]struct{} {
	return keySet(
		BotTokenKey,
		YooKassaTokenKey,
		StripeTokenKey,
		PayMasterTokenKey,
		CryptoBotTokenKey,
		ProxyURLKey,
		ProxyUsernameKey,
		ProxyPasswordKey,
	)
}

func InternalKeys() map[string]struct{} {
	return keySet(UpdateOffsetKey)
}

func keySet(keys ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return set
}
