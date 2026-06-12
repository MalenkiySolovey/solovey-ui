package service

const (
	settingKeyPaidSubEnabled              = "paidSubEnabled"
	settingKeyPaidSubBotToken             = "paidSubBotToken"
	settingKeyPaidSubBotPollSeconds       = "paidSubBotPollSeconds"
	settingKeyPaidSubUpdateOffset         = "paidSubUpdateOffset"
	settingKeyPaidSubTransportMode        = "paidSubTransportMode"
	settingKeyPaidSubProxyURL             = "paidSubProxyURL"
	settingKeyPaidSubProxyUsername        = "paidSubProxyUsername"
	settingKeyPaidSubProxyPassword        = "paidSubProxyPassword"
	settingKeyPaidSubOutboundTag          = "paidSubOutboundTag"
	settingKeyPaidSubAutoRegister         = "paidSubAutoRegister"
	settingKeyPaidSubAutoInbounds         = "paidSubAutoInbounds"
	settingKeyPaidSubTrialDays            = "paidSubTrialDays"
	settingKeyPaidSubTrialVolumeGB        = "paidSubTrialVolumeGB"
	settingKeyPaidSubMaxClients           = "paidSubMaxClients"
	settingKeyPaidSubStartRateLimitPerMin = "paidSubStartRateLimitPerMin"
	settingKeyPaidSubCurrency             = "paidSubCurrency"
	settingKeyPaidSubStarsEnabled         = "paidSubStarsEnabled"
	settingKeyPaidSubYooKassaEnabled      = "paidSubYooKassaEnabled"
	settingKeyPaidSubYooKassaToken        = "paidSubYooKassaToken"
	settingKeyPaidSubStripeEnabled        = "paidSubStripeEnabled"
	settingKeyPaidSubStripeToken          = "paidSubStripeToken"
	settingKeyPaidSubPayMasterEnabled     = "paidSubPayMasterEnabled"
	settingKeyPaidSubPayMasterToken       = "paidSubPayMasterToken"
	settingKeyPaidSubCryptoBotEnabled     = "paidSubCryptoBotEnabled"
	settingKeyPaidSubCryptoBotToken       = "paidSubCryptoBotToken"
	settingKeyPaidSubExternalEnabled      = "paidSubExternalEnabled"
	settingKeyPaidSubExternalURLTemplate  = "paidSubExternalUrlTemplate"
	settingKeyPaidSubOrderTTLMinutes      = "paidSubOrderTTLMinutes"
	settingKeyPaidSubGreeting             = "paidSubGreeting"
	settingKeyPaidSubRefundRevoke         = "paidSubRefundRevoke"
)

var defaultPaidSubSettingValues = map[string]string{
	settingKeyPaidSubEnabled:              "false",
	settingKeyPaidSubBotToken:             "",
	settingKeyPaidSubBotPollSeconds:       "25",
	settingKeyPaidSubUpdateOffset:         "0",
	settingKeyPaidSubTransportMode:        "proxy",
	settingKeyPaidSubProxyURL:             "",
	settingKeyPaidSubProxyUsername:        "",
	settingKeyPaidSubProxyPassword:        "",
	settingKeyPaidSubOutboundTag:          "",
	settingKeyPaidSubAutoRegister:         "false",
	settingKeyPaidSubAutoInbounds:         "[]",
	settingKeyPaidSubTrialDays:            "3",
	settingKeyPaidSubTrialVolumeGB:        "0",
	settingKeyPaidSubMaxClients:           "5000",
	settingKeyPaidSubStartRateLimitPerMin: "3",
	settingKeyPaidSubCurrency:             "RUB",
	settingKeyPaidSubStarsEnabled:         "false",
	settingKeyPaidSubYooKassaEnabled:      "false",
	settingKeyPaidSubYooKassaToken:        "",
	settingKeyPaidSubStripeEnabled:        "false",
	settingKeyPaidSubStripeToken:          "",
	settingKeyPaidSubPayMasterEnabled:     "false",
	settingKeyPaidSubPayMasterToken:       "",
	settingKeyPaidSubCryptoBotEnabled:     "false",
	settingKeyPaidSubCryptoBotToken:       "",
	settingKeyPaidSubExternalEnabled:      "false",
	settingKeyPaidSubExternalURLTemplate:  "",
	settingKeyPaidSubOrderTTLMinutes:      "30",
	settingKeyPaidSubGreeting:             "",
	settingKeyPaidSubRefundRevoke:         "true",
}

var paidSubBooleanSettingKeys = settingKeySet(
	settingKeyPaidSubEnabled,
	settingKeyPaidSubAutoRegister,
	settingKeyPaidSubStarsEnabled,
	settingKeyPaidSubYooKassaEnabled,
	settingKeyPaidSubStripeEnabled,
	settingKeyPaidSubPayMasterEnabled,
	settingKeyPaidSubCryptoBotEnabled,
	settingKeyPaidSubExternalEnabled,
	settingKeyPaidSubRefundRevoke,
)

var paidSubEncryptedSettingKeys = settingKeySet(
	settingKeyPaidSubBotToken,
	settingKeyPaidSubYooKassaToken,
	settingKeyPaidSubStripeToken,
	settingKeyPaidSubPayMasterToken,
	settingKeyPaidSubCryptoBotToken,
	settingKeyPaidSubProxyURL,
	settingKeyPaidSubProxyUsername,
	settingKeyPaidSubProxyPassword,
)
