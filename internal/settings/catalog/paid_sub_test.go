package catalog

import "testing"

func TestPaidSubDefaults(t *testing.T) {
	defaults := PaidSubDefaults()
	if defaults[PaidSubBotPollSecondsKey] != "25" {
		t.Fatalf("paid sub poll default = %q", defaults[PaidSubBotPollSecondsKey])
	}
	if defaults[PaidSubCurrencyKey] != "RUB" {
		t.Fatalf("paid sub currency default = %q", defaults[PaidSubCurrencyKey])
	}
	if defaults[PaidSubRefundRevokeKey] != "true" {
		t.Fatalf("paid sub refund revoke default = %q", defaults[PaidSubRefundRevokeKey])
	}
	if defaults[PaidSubExternalURLTemplateKey] != "" {
		t.Fatalf("paid sub external URL template default = %q", defaults[PaidSubExternalURLTemplateKey])
	}
}

func TestPaidSubKeyGroups(t *testing.T) {
	if _, ok := PaidSubBooleanKeys()[PaidSubStarsEnabledKey]; !ok {
		t.Fatal("paid sub stars enabled should be a boolean key")
	}
	if _, ok := PaidSubBooleanKeys()[PaidSubBotTokenKey]; ok {
		t.Fatal("paid sub bot token should not be a boolean key")
	}
	if _, ok := PaidSubEncryptedKeys()[PaidSubBotTokenKey]; !ok {
		t.Fatal("paid sub bot token should be encrypted")
	}
	if _, ok := PaidSubEncryptedKeys()[PaidSubProxyPasswordKey]; !ok {
		t.Fatal("paid sub proxy password should be encrypted")
	}
}
