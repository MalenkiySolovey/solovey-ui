package validation

import (
	"strings"
	"testing"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
)

func TestValidatePaidSubSettingInput(t *testing.T) {
	valid := map[string]string{
		settingcatalog.PaidSubEnabledKey:              "true",
		settingcatalog.PaidSubBotPollSecondsKey:       "25",
		settingcatalog.PaidSubTrialDaysKey:            "0",
		settingcatalog.PaidSubTrialVolumeGBKey:        "1048576",
		settingcatalog.PaidSubMaxClientsKey:           "5000",
		settingcatalog.PaidSubStartRateLimitPerMinKey: "3",
		settingcatalog.PaidSubOrderTTLMinutesKey:      "30",
		settingcatalog.PaidSubAutoInboundsKey:         "[1,2]",
		settingcatalog.PaidSubCurrencyKey:             "rub",
		settingcatalog.PaidSubExternalURLTemplateKey:  "https://example.com/order/{id}",
		settingcatalog.PaidSubTransportModeKey:        "proxy",
		settingcatalog.PaidSubOutboundTagKey:          strings.Repeat("a", 256),
		settingcatalog.PaidSubGreetingKey:             strings.Repeat("я", 4096),
	}
	for key, value := range valid {
		if err := ValidatePaidSubSettingInput(key, value); err != nil {
			t.Fatalf("valid paidSub setting %s rejected: %v", key, err)
		}
	}

	invalid := map[string]string{
		settingcatalog.PaidSubEnabledKey:              "sometimes",
		settingcatalog.PaidSubBotPollSecondsKey:       "0",
		settingcatalog.PaidSubTrialDaysKey:            "-1",
		settingcatalog.PaidSubTrialVolumeGBKey:        "1048577",
		settingcatalog.PaidSubMaxClientsKey:           "10000001",
		settingcatalog.PaidSubStartRateLimitPerMinKey: "1001",
		settingcatalog.PaidSubOrderTTLMinutesKey:      "1441",
		settingcatalog.PaidSubAutoInboundsKey:         `{"id":1}`,
		settingcatalog.PaidSubCurrencyKey:             "RUBLE",
		settingcatalog.PaidSubExternalURLTemplateKey:  "http://example.com/order",
		settingcatalog.PaidSubTransportModeKey:        "direct",
		settingcatalog.PaidSubOutboundTagKey:          strings.Repeat("a", 257),
		settingcatalog.PaidSubGreetingKey:             strings.Repeat("я", 4097),
	}
	for key, value := range invalid {
		if err := ValidatePaidSubSettingInput(key, value); err == nil {
			t.Fatalf("invalid paidSub setting %s accepted", key)
		}
	}
}

func TestValidatePaidSubExternalURLTemplateRejectsSpacesAndFragments(t *testing.T) {
	for _, value := range []string{
		"https://example.com/order with-space",
		"https://example.com/order#fragment",
		"https://example.com/order\nnext",
	} {
		if err := ValidatePaidSubSettingInput(settingcatalog.PaidSubExternalURLTemplateKey, value); err == nil {
			t.Fatalf("invalid paidSub external URL template accepted: %q", value)
		}
	}
}
