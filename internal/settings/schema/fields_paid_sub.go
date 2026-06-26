package schema

import settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"

func paidSubFieldMetadata() []Field {
	return []Field{
		{Key: settingcatalog.PaidSubEnabledKey, Page: PagePaidSub, Group: GroupPaidSubBot, Type: FieldTypeBool, LabelKey: "paidSub.bot.enable", Order: 10},
		{Key: settingcatalog.PaidSubBotTokenKey, Page: PagePaidSub, Group: GroupPaidSubBot, Type: FieldTypeSecret, LabelKey: "paidSub.bot.token", Order: 20},
		{Key: settingcatalog.PaidSubBotPollSecondsKey, Page: PagePaidSub, Group: GroupPaidSubBot, Type: FieldTypeInt, LabelKey: "paidSub.bot.pollTimeout", Min: intPtr(1), Order: 30},
		{Key: settingcatalog.PaidSubTransportModeKey, Page: PagePaidSub, Group: GroupPaidSubBot, Type: FieldTypeEnum, LabelKey: "paidSub.bot.transport", Options: []string{"proxy", "outbound"}, Order: 40},
		{Key: settingcatalog.PaidSubProxyURLKey, Page: PagePaidSub, Group: GroupPaidSubBot, Type: FieldTypeSecret, LabelKey: "paidSub.bot.proxyUrl", Advanced: true, Order: 50},
		{Key: settingcatalog.PaidSubProxyUsernameKey, Page: PagePaidSub, Group: GroupPaidSubBot, Type: FieldTypeSecret, LabelKey: "paidSub.bot.proxyUser", Advanced: true, Order: 60},
		{Key: settingcatalog.PaidSubProxyPasswordKey, Page: PagePaidSub, Group: GroupPaidSubBot, Type: FieldTypeSecret, LabelKey: "paidSub.bot.proxyPass", Advanced: true, Order: 70},
		{Key: settingcatalog.PaidSubOutboundTagKey, Page: PagePaidSub, Group: GroupPaidSubBot, Type: FieldTypeString, LabelKey: "paidSub.bot.outbound", Order: 80},
		{Key: settingcatalog.PaidSubUpdateOffsetKey, Page: PageInternal, Group: GroupInternal, Type: FieldTypeInt, LabelKey: "paidSub.bot.updateOffset", Order: 90},

		{Key: settingcatalog.PaidSubAutoRegisterKey, Page: PagePaidSub, Group: GroupPaidSubAutoreg, Type: FieldTypeBool, LabelKey: "paidSub.autoreg.enable", Order: 10},
		{Key: settingcatalog.PaidSubAutoInboundsKey, Page: PagePaidSub, Group: GroupPaidSubAutoreg, Type: FieldTypeJSON, LabelKey: "paidSub.autoreg.inbounds", Order: 20},
		{Key: settingcatalog.PaidSubTrialDaysKey, Page: PagePaidSub, Group: GroupPaidSubAutoreg, Type: FieldTypeInt, LabelKey: "paidSub.autoreg.trialDays", Min: intPtr(0), Order: 30},
		{Key: settingcatalog.PaidSubTrialVolumeGBKey, Page: PagePaidSub, Group: GroupPaidSubAutoreg, Type: FieldTypeInt, LabelKey: "paidSub.autoreg.trialVolume", Min: intPtr(0), Order: 40},
		{Key: settingcatalog.PaidSubMaxClientsKey, Page: PagePaidSub, Group: GroupPaidSubAutoreg, Type: FieldTypeInt, LabelKey: "paidSub.autoreg.maxClients", Min: intPtr(1), Order: 50},
		{Key: settingcatalog.PaidSubStartRateLimitPerMinKey, Page: PagePaidSub, Group: GroupPaidSubAutoreg, Type: FieldTypeInt, LabelKey: "paidSub.autoreg.rateLimit", Min: intPtr(0), Order: 60},

		{Key: settingcatalog.PaidSubCurrencyKey, Page: PagePaidSub, Group: GroupPaidSubPayments, Type: FieldTypeString, LabelKey: "paidSub.payments.currency", Order: 10},
		{Key: settingcatalog.PaidSubStarsEnabledKey, Page: PagePaidSub, Group: GroupPaidSubPayments, Type: FieldTypeBool, LabelKey: "paidSub.payments.stars", Order: 20},
		{Key: settingcatalog.PaidSubYooKassaEnabledKey, Page: PagePaidSub, Group: GroupPaidSubPayments, Type: FieldTypeBool, LabelKey: "paidSub.payments.yookassa", Order: 30},
		{Key: settingcatalog.PaidSubYooKassaTokenKey, Page: PagePaidSub, Group: GroupPaidSubPayments, Type: FieldTypeSecret, LabelKey: "paidSub.payments.yookassaToken", Order: 40},
		{Key: settingcatalog.PaidSubStripeEnabledKey, Page: PagePaidSub, Group: GroupPaidSubPayments, Type: FieldTypeBool, LabelKey: "paidSub.payments.stripe", Order: 50},
		{Key: settingcatalog.PaidSubStripeTokenKey, Page: PagePaidSub, Group: GroupPaidSubPayments, Type: FieldTypeSecret, LabelKey: "paidSub.payments.stripeToken", Order: 60},
		{Key: settingcatalog.PaidSubPayMasterEnabledKey, Page: PagePaidSub, Group: GroupPaidSubPayments, Type: FieldTypeBool, LabelKey: "paidSub.payments.paymaster", Order: 70},
		{Key: settingcatalog.PaidSubPayMasterTokenKey, Page: PagePaidSub, Group: GroupPaidSubPayments, Type: FieldTypeSecret, LabelKey: "paidSub.payments.paymasterToken", Order: 80},
		{Key: settingcatalog.PaidSubCryptoBotEnabledKey, Page: PagePaidSub, Group: GroupPaidSubPayments, Type: FieldTypeBool, LabelKey: "paidSub.payments.crypto", Order: 90},
		{Key: settingcatalog.PaidSubCryptoBotTokenKey, Page: PagePaidSub, Group: GroupPaidSubPayments, Type: FieldTypeSecret, LabelKey: "paidSub.payments.cryptoToken", Order: 100},
		{Key: settingcatalog.PaidSubExternalEnabledKey, Page: PagePaidSub, Group: GroupPaidSubPayments, Type: FieldTypeBool, LabelKey: "paidSub.payments.external", Order: 110},
		{Key: settingcatalog.PaidSubExternalURLTemplateKey, Page: PagePaidSub, Group: GroupPaidSubPayments, Type: FieldTypeString, LabelKey: "paidSub.payments.externalTemplate", Order: 120},
		{Key: settingcatalog.PaidSubOrderTTLMinutesKey, Page: PagePaidSub, Group: GroupPaidSubPayments, Type: FieldTypeInt, LabelKey: "paidSub.payments.orderTtl", Min: intPtr(1), Order: 130},
		{Key: settingcatalog.PaidSubRefundRevokeKey, Page: PagePaidSub, Group: GroupPaidSubPayments, Type: FieldTypeBool, LabelKey: "paidSub.refund.revoke", Advanced: true, Order: 140},

		{Key: settingcatalog.PaidSubGreetingKey, Page: PagePaidSub, Group: GroupPaidSubMessages, Type: FieldTypeText, LabelKey: "paidSub.messages.greetingLabel", Order: 10},
	}
}
