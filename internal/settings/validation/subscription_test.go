package validation

import (
	"testing"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
)

func TestValidateSubscriptionPaths(t *testing.T) {
	valid := []SubscriptionPaths{
		{Base: "/sub/", JSON: "/json/", Clash: "/clash/", Xray: "/xray/"},
		{Base: "/", JSON: "/json/", Clash: "/clash/", Xray: "/xray/"},
	}
	for _, paths := range valid {
		if err := ValidateSubscriptionPaths(paths); err != nil {
			t.Fatalf("valid subscription paths rejected: %+v: %v", paths, err)
		}
	}

	invalid := []SubscriptionPaths{
		{Base: "/sub/", JSON: "/json/", Clash: "/json/", Xray: "/xray/"},
		{Base: "/sub/", JSON: "/json/", Clash: "/clash/", Xray: "/clash/"},
		{Base: "/sub/", JSON: "/", Clash: "/clash/", Xray: "/xray/"},
		{Base: "/sub/", JSON: "/sub/json/", Clash: "/clash/", Xray: "/xray/"},
	}
	for _, paths := range invalid {
		if err := ValidateSubscriptionPaths(paths); err == nil {
			t.Fatalf("invalid subscription paths accepted: %+v", paths)
		}
	}
}

func TestValidateSubscriptionSettingInput(t *testing.T) {
	valid := map[string]string{
		settingcatalog.SubJsonEnableKey:            "false",
		settingcatalog.SubXrayEnableKey:            "true",
		settingcatalog.SubXrayURIKey:               "https://xray.example/sub/",
		settingcatalog.SubSupportURLKey:            "https://example.com/support",
		settingcatalog.SubRateLimitPerIPKey:        "120",
		settingcatalog.SubJsonFragmentKey:          `{"enabled":true}`,
		settingcatalog.SubJsonNoisesKey:            `[{"type":"rand"}]`,
		settingcatalog.SubRemoteGroupAdaptationKey: "failover",
	}
	for key, value := range valid {
		if err := ValidateSubscriptionSettingInput(key, value); err != nil {
			t.Fatalf("valid subscription setting %s rejected: %v", key, err)
		}
	}

	invalid := map[string]string{
		settingcatalog.SubJsonEnableKey:            "sometimes",
		settingcatalog.SubSupportURLKey:            "ftp://example.com/support",
		settingcatalog.SubRateLimitPerIPKey:        "0",
		settingcatalog.SubJsonFragmentKey:          "enabled",
		settingcatalog.SubJsonNoisesKey:            `{"type":"rand"}`,
		settingcatalog.SubRemoteGroupAdaptationKey: "relay",
	}
	for key, value := range invalid {
		if err := ValidateSubscriptionSettingInput(key, value); err == nil {
			t.Fatalf("invalid subscription setting %s accepted", key)
		}
	}
}
