package service

import "testing"

func TestValidateSubscriptionPathSettingsRejectsPhase2Conflicts(t *testing.T) {
	settingService := &SettingService{}
	tests := []map[string]string{
		{
			"subPath":      "/sub/",
			"subJsonPath":  "/same/",
			"subClashPath": "/same/",
		},
		{
			"subPath":      "/subscriptions/",
			"subJsonPath":  "/subscriptions/json/",
			"subClashPath": "/clash/",
		},
	}
	for _, settings := range tests {
		if err := settingService.validateSubscriptionPathSettings(settings); err == nil {
			t.Fatalf("expected subscription path conflict for %#v", settings)
		}
	}
}

func TestValidateTelegramSettingInputRejectsWeakBackupPassphrase(t *testing.T) {
	if err := validateTelegramSettingInput("telegramBackupPassphrase", "too-short"); err == nil {
		t.Fatal("weak telegram backup passphrase should be rejected")
	}
	if err := validateTelegramSettingInput("telegramBackupPassphrase", "correct horse battery staple"); err != nil {
		t.Fatalf("strong telegram backup passphrase should be accepted: %v", err)
	}
}

func TestValidateOptionalHTTPURLRejectsUserInfo(t *testing.T) {
	if err := validateOptionalHTTPURL("https://user:pass@example.com/profile"); err == nil {
		t.Fatal("URL with user-info should be rejected")
	}
	if err := validateOptionalHTTPURL("https://example.com/profile"); err != nil {
		t.Fatalf("plain HTTPS URL should be accepted: %v", err)
	}
}

func TestValidateOptionalHTTPURLRejectsFragment_XFAILIssue30(t *testing.T) {
	t.Skip("XFAIL: issue 30; validateOptionalHTTPURL currently accepts URL fragments")

	if err := validateOptionalHTTPURL("https://example.com/profile#token"); err == nil {
		t.Fatal("URL fragment should be rejected")
	}
}
