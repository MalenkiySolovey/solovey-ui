package service

import settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"

const (
	settingKeyPaidSubEnabled              = settingcatalog.PaidSubEnabledKey
	settingKeyPaidSubBotToken             = settingcatalog.PaidSubBotTokenKey
	settingKeyPaidSubBotPollSeconds       = settingcatalog.PaidSubBotPollSecondsKey
	settingKeyPaidSubUpdateOffset         = settingcatalog.PaidSubUpdateOffsetKey
	settingKeyPaidSubTransportMode        = settingcatalog.PaidSubTransportModeKey
	settingKeyPaidSubProxyURL             = settingcatalog.PaidSubProxyURLKey
	settingKeyPaidSubProxyUsername        = settingcatalog.PaidSubProxyUsernameKey
	settingKeyPaidSubProxyPassword        = settingcatalog.PaidSubProxyPasswordKey
	settingKeyPaidSubOutboundTag          = settingcatalog.PaidSubOutboundTagKey
	settingKeyPaidSubAutoRegister         = settingcatalog.PaidSubAutoRegisterKey
	settingKeyPaidSubAutoInbounds         = settingcatalog.PaidSubAutoInboundsKey
	settingKeyPaidSubTrialDays            = settingcatalog.PaidSubTrialDaysKey
	settingKeyPaidSubTrialVolumeGB        = settingcatalog.PaidSubTrialVolumeGBKey
	settingKeyPaidSubMaxClients           = settingcatalog.PaidSubMaxClientsKey
	settingKeyPaidSubStartRateLimitPerMin = settingcatalog.PaidSubStartRateLimitPerMinKey
	settingKeyPaidSubCurrency             = settingcatalog.PaidSubCurrencyKey
	settingKeyPaidSubStarsEnabled         = settingcatalog.PaidSubStarsEnabledKey
	settingKeyPaidSubYooKassaEnabled      = settingcatalog.PaidSubYooKassaEnabledKey
	settingKeyPaidSubYooKassaToken        = settingcatalog.PaidSubYooKassaTokenKey
	settingKeyPaidSubStripeEnabled        = settingcatalog.PaidSubStripeEnabledKey
	settingKeyPaidSubStripeToken          = settingcatalog.PaidSubStripeTokenKey
	settingKeyPaidSubPayMasterEnabled     = settingcatalog.PaidSubPayMasterEnabledKey
	settingKeyPaidSubPayMasterToken       = settingcatalog.PaidSubPayMasterTokenKey
	settingKeyPaidSubCryptoBotEnabled     = settingcatalog.PaidSubCryptoBotEnabledKey
	settingKeyPaidSubCryptoBotToken       = settingcatalog.PaidSubCryptoBotTokenKey
	settingKeyPaidSubExternalEnabled      = settingcatalog.PaidSubExternalEnabledKey
	settingKeyPaidSubExternalURLTemplate  = settingcatalog.PaidSubExternalURLTemplateKey
	settingKeyPaidSubOrderTTLMinutes      = settingcatalog.PaidSubOrderTTLMinutesKey
	settingKeyPaidSubGreeting             = settingcatalog.PaidSubGreetingKey
	settingKeyPaidSubRefundRevoke         = settingcatalog.PaidSubRefundRevokeKey
)

var defaultPaidSubSettingValues = settingcatalog.PaidSubDefaults()

var paidSubEncryptedSettingKeys = settingcatalog.PaidSubEncryptedKeys()
