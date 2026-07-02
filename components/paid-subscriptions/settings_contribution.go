//go:build !minimal

package paidsubscriptions

import (
	"encoding/json"
	"strconv"
	"strings"

	paidsettings "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/settings"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"
	settingsvalidation "github.com/MalenkiySolovey/solovey-ui/internal/settings/validation"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

const (
	paidSubSettingsPage  = "paid_sub"
	paidSubBotGroup      = "paid_sub_bot"
	paidSubAutoregGroup  = "paid_sub_autoreg"
	paidSubPaymentsGroup = "paid_sub_payments"
	paidSubMessagesGroup = "paid_sub_messages"
	paidSubInternalPage  = "internal"
	paidSubInternalGroup = "internal"
)

func registerSettingContribution() func() {
	return service.RegisterSettingContribution(id, service.SettingContribution{
		Defaults:  paidsettings.Defaults(),
		Internal:  paidsettings.InternalKeys(),
		Encrypted: paidsettings.EncryptedKeys(),
		Fields:    paidSubSettingFields(),
		Validators: []service.SettingValidator{
			validatePaidSubSettingInput,
		},
	})
}

func paidSubSettingFields() []settingsschema.Field {
	return []settingsschema.Field{
		{Key: paidsettings.EnabledKey, Page: paidSubSettingsPage, Group: paidSubBotGroup, Type: settingsschema.FieldTypeBool, LabelKey: "paidSub.bot.enable", Order: 10},
		{Key: paidsettings.BotTokenKey, Page: paidSubSettingsPage, Group: paidSubBotGroup, Type: settingsschema.FieldTypeSecret, LabelKey: "paidSub.bot.token", Order: 20},
		{Key: paidsettings.BotPollSecondsKey, Page: paidSubSettingsPage, Group: paidSubBotGroup, Type: settingsschema.FieldTypeInt, LabelKey: "paidSub.bot.pollTimeout", Min: intPtr(1), Order: 30},
		{Key: paidsettings.TransportModeKey, Page: paidSubSettingsPage, Group: paidSubBotGroup, Type: settingsschema.FieldTypeEnum, LabelKey: "paidSub.bot.transport", Options: []string{"proxy", "outbound"}, Order: 40},
		{Key: paidsettings.ProxyURLKey, Page: paidSubSettingsPage, Group: paidSubBotGroup, Type: settingsschema.FieldTypeSecret, LabelKey: "paidSub.bot.proxyUrl", Advanced: true, Order: 50},
		{Key: paidsettings.ProxyUsernameKey, Page: paidSubSettingsPage, Group: paidSubBotGroup, Type: settingsschema.FieldTypeSecret, LabelKey: "paidSub.bot.proxyUser", Advanced: true, Order: 60},
		{Key: paidsettings.ProxyPasswordKey, Page: paidSubSettingsPage, Group: paidSubBotGroup, Type: settingsschema.FieldTypeSecret, LabelKey: "paidSub.bot.proxyPass", Advanced: true, Order: 70},
		{Key: paidsettings.OutboundTagKey, Page: paidSubSettingsPage, Group: paidSubBotGroup, Type: settingsschema.FieldTypeString, LabelKey: "paidSub.bot.outbound", Order: 80},
		{Key: paidsettings.UpdateOffsetKey, Page: paidSubInternalPage, Group: paidSubInternalGroup, Type: settingsschema.FieldTypeInt, LabelKey: "paidSub.bot.updateOffset", Order: 90},

		{Key: paidsettings.AutoRegisterKey, Page: paidSubSettingsPage, Group: paidSubAutoregGroup, Type: settingsschema.FieldTypeBool, LabelKey: "paidSub.autoreg.enable", Order: 10},
		{Key: paidsettings.AutoInboundsKey, Page: paidSubSettingsPage, Group: paidSubAutoregGroup, Type: settingsschema.FieldTypeJSON, LabelKey: "paidSub.autoreg.inbounds", Order: 20},
		{Key: paidsettings.TrialDaysKey, Page: paidSubSettingsPage, Group: paidSubAutoregGroup, Type: settingsschema.FieldTypeInt, LabelKey: "paidSub.autoreg.trialDays", Min: intPtr(0), Order: 30},
		{Key: paidsettings.TrialVolumeGBKey, Page: paidSubSettingsPage, Group: paidSubAutoregGroup, Type: settingsschema.FieldTypeInt, LabelKey: "paidSub.autoreg.trialVolume", Min: intPtr(0), Order: 40},
		{Key: paidsettings.MaxClientsKey, Page: paidSubSettingsPage, Group: paidSubAutoregGroup, Type: settingsschema.FieldTypeInt, LabelKey: "paidSub.autoreg.maxClients", Min: intPtr(1), Order: 50},
		{Key: paidsettings.StartRateLimitPerMinKey, Page: paidSubSettingsPage, Group: paidSubAutoregGroup, Type: settingsschema.FieldTypeInt, LabelKey: "paidSub.autoreg.rateLimit", Min: intPtr(0), Order: 60},

		{Key: paidsettings.CurrencyKey, Page: paidSubSettingsPage, Group: paidSubPaymentsGroup, Type: settingsschema.FieldTypeString, LabelKey: "paidSub.payments.currency", Order: 10},
		{Key: paidsettings.StarsEnabledKey, Page: paidSubSettingsPage, Group: paidSubPaymentsGroup, Type: settingsschema.FieldTypeBool, LabelKey: "paidSub.payments.stars", Order: 20},
		{Key: paidsettings.YooKassaEnabledKey, Page: paidSubSettingsPage, Group: paidSubPaymentsGroup, Type: settingsschema.FieldTypeBool, LabelKey: "paidSub.payments.yookassa", Order: 30},
		{Key: paidsettings.YooKassaTokenKey, Page: paidSubSettingsPage, Group: paidSubPaymentsGroup, Type: settingsschema.FieldTypeSecret, LabelKey: "paidSub.payments.yookassaToken", Order: 40},
		{Key: paidsettings.StripeEnabledKey, Page: paidSubSettingsPage, Group: paidSubPaymentsGroup, Type: settingsschema.FieldTypeBool, LabelKey: "paidSub.payments.stripe", Order: 50},
		{Key: paidsettings.StripeTokenKey, Page: paidSubSettingsPage, Group: paidSubPaymentsGroup, Type: settingsschema.FieldTypeSecret, LabelKey: "paidSub.payments.stripeToken", Order: 60},
		{Key: paidsettings.PayMasterEnabledKey, Page: paidSubSettingsPage, Group: paidSubPaymentsGroup, Type: settingsschema.FieldTypeBool, LabelKey: "paidSub.payments.paymaster", Order: 70},
		{Key: paidsettings.PayMasterTokenKey, Page: paidSubSettingsPage, Group: paidSubPaymentsGroup, Type: settingsschema.FieldTypeSecret, LabelKey: "paidSub.payments.paymasterToken", Order: 80},
		{Key: paidsettings.CryptoBotEnabledKey, Page: paidSubSettingsPage, Group: paidSubPaymentsGroup, Type: settingsschema.FieldTypeBool, LabelKey: "paidSub.payments.crypto", Order: 90},
		{Key: paidsettings.CryptoBotTokenKey, Page: paidSubSettingsPage, Group: paidSubPaymentsGroup, Type: settingsschema.FieldTypeSecret, LabelKey: "paidSub.payments.cryptoToken", Order: 100},
		{Key: paidsettings.ExternalEnabledKey, Page: paidSubSettingsPage, Group: paidSubPaymentsGroup, Type: settingsschema.FieldTypeBool, LabelKey: "paidSub.payments.external", Order: 110},
		{Key: paidsettings.ExternalURLTemplateKey, Page: paidSubSettingsPage, Group: paidSubPaymentsGroup, Type: settingsschema.FieldTypeString, LabelKey: "paidSub.payments.externalTemplate", Order: 120},
		{Key: paidsettings.OrderTTLMinutesKey, Page: paidSubSettingsPage, Group: paidSubPaymentsGroup, Type: settingsschema.FieldTypeInt, LabelKey: "paidSub.payments.orderTtl", Min: intPtr(1), Order: 130},
		{Key: paidsettings.RefundRevokeKey, Page: paidSubSettingsPage, Group: paidSubPaymentsGroup, Type: settingsschema.FieldTypeBool, LabelKey: "paidSub.refund.revoke", Advanced: true, Order: 140},

		{Key: paidsettings.GreetingKey, Page: paidSubSettingsPage, Group: paidSubMessagesGroup, Type: settingsschema.FieldTypeText, LabelKey: "paidSub.messages.greetingLabel", Order: 10},
	}
}

func validatePaidSubSettingInput(key string, value string, storedSecretMarker string) error {
	if _, ok := paidsettings.BooleanKeys()[key]; ok {
		if _, err := strconv.ParseBool(value); err != nil {
			return common.NewError("invalid boolean setting: ", key)
		}
		return nil
	}
	switch key {
	case paidsettings.BotPollSecondsKey:
		if err := settingsvalidation.ValidateIntRange(key, value, 1, 50); err != nil {
			return err
		}
	case paidsettings.TrialDaysKey:
		if err := settingsvalidation.ValidateIntRange(key, value, 0, 3650); err != nil {
			return err
		}
	case paidsettings.TrialVolumeGBKey:
		if err := settingsvalidation.ValidateIntRange(key, value, 0, 1048576); err != nil {
			return err
		}
	case paidsettings.MaxClientsKey:
		if err := settingsvalidation.ValidateIntRange(key, value, 0, 10000000); err != nil {
			return err
		}
	case paidsettings.StartRateLimitPerMinKey:
		if err := settingsvalidation.ValidateIntRange(key, value, 0, 1000); err != nil {
			return err
		}
	case paidsettings.OrderTTLMinutesKey:
		if err := settingsvalidation.ValidateIntRange(key, value, 1, 1440); err != nil {
			return err
		}
	case paidsettings.AutoInboundsKey:
		if value != "" {
			var ids []uint
			if err := json.Unmarshal([]byte(value), &ids); err != nil {
				return common.NewError("paidSubAutoInbounds must be a JSON array of inbound ids")
			}
		}
	case paidsettings.CurrencyKey:
		v := strings.ToUpper(strings.TrimSpace(value))
		if len(v) != 3 {
			return common.NewError("paidSubCurrency must be a 3-letter code")
		}
	case paidsettings.ExternalURLTemplateKey:
		if value != "" {
			if len(value) > 2048 {
				return common.NewError("paidSubExternalUrlTemplate is too long")
			}
			if !strings.HasPrefix(value, "https://") {
				return common.NewError("paidSubExternalUrlTemplate must start with https://")
			}
			if strings.ContainsAny(value, " \t\r\n#") {
				return common.NewError("paidSubExternalUrlTemplate must not contain spaces or a fragment")
			}
		}
	case paidsettings.TransportModeKey:
		if err := settingsvalidation.ValidateTransportMode(value); err != nil {
			return err
		}
	case paidsettings.OutboundTagKey:
		if len(value) > 256 {
			return common.NewError("paidSubOutboundTag is too long")
		}
	case paidsettings.GreetingKey:
		if len([]rune(value)) > 4096 {
			return common.NewError("paidSubGreeting is too long (max 4096)")
		}
	case paidsettings.ProxyURLKey:
		if err := settingsvalidation.ValidateProxyURLValue(value, storedSecretMarker); err != nil {
			return err
		}
	}
	return nil
}

func intPtr(value int) *int {
	return &value
}
