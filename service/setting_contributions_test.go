package service

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	settingsvalidation "github.com/MalenkiySolovey/solovey-ui/internal/settings/validation"
)

const (
	testTelegramEnabledKey             = "telegramEnabled"
	testTelegramBotTokenKey            = "telegramBotToken"
	testTelegramProxyURLKey            = "telegramProxyURL"
	testTelegramProxyUsernameKey       = "telegramProxyUsername"
	testTelegramProxyPasswordKey       = "telegramProxyPassword"
	testTelegramTransportModeKey       = "telegramTransportMode"
	testTelegramOutboundTagKey         = "telegramOutboundTag"
	testTelegramCPUThresholdKey        = "telegramCpuThreshold"
	testTelegramNotifyCPUKey           = "telegramNotifyCpu"
	testTelegramReportKey              = "telegramReport"
	testTelegramReportCronKey          = "telegramReportCron"
	testTelegramBackupEnabledKey       = "telegramBackupEnabled"
	testTelegramBackupPassphraseKey    = "telegramBackupPassphrase"
	testTelegramBackupCronKey          = "telegramBackupCron"
	testTelegramBackupExcludeTablesKey = "telegramBackupExcludeTables"
	testTelegramBackupMaxSizeMBKey     = "telegramBackupMaxSizeMB"
	testPaidSubEnabledKey              = "paidSubEnabled"
	testPaidSubBotTokenKey             = "paidSubBotToken"
	testPaidSubBotPollSecondsKey       = "paidSubBotPollSeconds"
	testPaidSubUpdateOffsetKey         = "paidSubUpdateOffset"
	testPaidSubProxyURLKey             = "paidSubProxyURL"
	testPaidSubProxyUsernameKey        = "paidSubProxyUsername"
	testPaidSubProxyPasswordKey        = "paidSubProxyPassword"
	testPaidSubAutoRegisterKey         = "paidSubAutoRegister"
	testPaidSubAutoInboundsKey         = "paidSubAutoInbounds"
	testPaidSubStarsEnabledKey         = "paidSubStarsEnabled"
	testPaidSubYooKassaEnabledKey      = "paidSubYooKassaEnabled"
	testPaidSubYooKassaTokenKey        = "paidSubYooKassaToken"
	testPaidSubStripeEnabledKey        = "paidSubStripeEnabled"
	testPaidSubStripeTokenKey          = "paidSubStripeToken"
	testPaidSubPayMasterEnabledKey     = "paidSubPayMasterEnabled"
	testPaidSubPayMasterTokenKey       = "paidSubPayMasterToken"
	testPaidSubCryptoBotEnabledKey     = "paidSubCryptoBotEnabled"
	testPaidSubCryptoBotTokenKey       = "paidSubCryptoBotToken"
	testPaidSubExternalEnabledKey      = "paidSubExternalEnabled"
	testPaidSubRefundRevokeKey         = "paidSubRefundRevoke"
	testPaidSubCurrencyKey             = "paidSubCurrency"
)

func registerTelegramSettingsForTest(t *testing.T) {
	t.Helper()
	if _, ok := currentSettingsSchema().Default(testTelegramEnabledKey); ok {
		return
	}
	unregister := RegisterSettingContribution("test.telegram", SettingContribution{
		Defaults:                testTelegramDefaults(),
		Encrypted:               testTelegramEncryptedKeys(),
		ClearableEmptyEncrypted: settingKeySet(testTelegramBackupPassphraseKey),
		Validators: []SettingValidator{
			validateTelegramSettingForTest,
		},
	})
	t.Cleanup(unregister)
}

func registerPaidSubSettingsForTest(t *testing.T) {
	t.Helper()
	if _, ok := currentSettingsSchema().Default(testPaidSubEnabledKey); ok {
		return
	}
	unregister := RegisterSettingContribution("test.paid-subscriptions", SettingContribution{
		Defaults:  testPaidSubDefaults(),
		Internal:  settingKeySet(testPaidSubUpdateOffsetKey),
		Encrypted: testPaidSubEncryptedKeys(),
		Validators: []SettingValidator{
			validatePaidSubSettingForTest,
		},
	})
	t.Cleanup(unregister)
}

func registerSettingsPayloadContributionsForTest(t *testing.T) {
	t.Helper()
	registerTelegramSettingsForTest(t)
	registerPaidSubSettingsForTest(t)
}

func validateTelegramSettingForTest(key string, value string, storedSecretMarker string) error {
	if _, ok := testTelegramBooleanKeys()[key]; ok {
		_, err := strconv.ParseBool(value)
		return err
	}
	switch key {
	case testTelegramBackupEnabledKey:
		if value != "true" && value != "false" {
			return errors.New("invalid boolean setting")
		}
	case testTelegramCPUThresholdKey:
		threshold, err := strconv.Atoi(value)
		if err != nil || threshold <= 0 || threshold > 100 {
			return errors.New("invalid cpu threshold setting")
		}
	case testTelegramBackupPassphraseKey:
		if value != "" && value != storedSecretMarker && len([]rune(value)) < 12 {
			return errors.New("weak_passphrase")
		}
	case testTelegramProxyURLKey:
		return settingsvalidation.ValidateProxyURLValue(value, storedSecretMarker)
	}
	return nil
}

func testTelegramDefaults() map[string]string {
	return map[string]string{
		testTelegramEnabledKey:             "false",
		testTelegramBotTokenKey:            "",
		"telegramChatID":                   "",
		testTelegramProxyURLKey:            "",
		testTelegramProxyUsernameKey:       "",
		testTelegramProxyPasswordKey:       "",
		testTelegramTransportModeKey:       "proxy",
		testTelegramOutboundTagKey:         "",
		testTelegramCPUThresholdKey:        "90",
		testTelegramNotifyCPUKey:           "false",
		testTelegramReportKey:              "false",
		testTelegramReportCronKey:          "",
		testTelegramBackupEnabledKey:       "false",
		testTelegramBackupPassphraseKey:    "",
		testTelegramBackupCronKey:          "",
		testTelegramBackupExcludeTablesKey: "stats,client_ips,audit_events,changes",
		testTelegramBackupMaxSizeMBKey:     "45",
	}
}

func testTelegramBooleanKeys() map[string]struct{} {
	return settingKeySet(
		testTelegramNotifyCPUKey,
		testTelegramReportKey,
	)
}

func testTelegramEncryptedKeys() map[string]struct{} {
	return settingKeySet(
		testTelegramBackupPassphraseKey,
		testTelegramBotTokenKey,
		testTelegramProxyPasswordKey,
		testTelegramProxyURLKey,
		testTelegramProxyUsernameKey,
	)
}

func validatePaidSubSettingForTest(key string, value string, storedSecretMarker string) error {
	if _, ok := testPaidSubBooleanKeys()[key]; ok {
		_, err := strconv.ParseBool(value)
		return err
	}
	switch key {
	case testPaidSubAutoInboundsKey:
		if value == "" {
			return nil
		}
		var ids []uint
		return json.Unmarshal([]byte(value), &ids)
	case testPaidSubProxyURLKey:
		return settingsvalidation.ValidateProxyURLValue(value, storedSecretMarker)
	case testPaidSubCurrencyKey:
		if len(strings.ToUpper(strings.TrimSpace(value))) != 3 {
			return errors.New("paidSubCurrency must be a 3-letter code")
		}
	}
	return nil
}

func testPaidSubDefaults() map[string]string {
	return map[string]string{
		testPaidSubEnabledKey:          "false",
		testPaidSubBotTokenKey:         "",
		testPaidSubBotPollSecondsKey:   "25",
		testPaidSubUpdateOffsetKey:     "0",
		"paidSubTransportMode":         "proxy",
		testPaidSubProxyURLKey:         "",
		testPaidSubProxyUsernameKey:    "",
		testPaidSubProxyPasswordKey:    "",
		"paidSubOutboundTag":           "",
		testPaidSubAutoRegisterKey:     "false",
		testPaidSubAutoInboundsKey:     "[]",
		"paidSubTrialDays":             "3",
		"paidSubTrialVolumeGB":         "0",
		"paidSubMaxClients":            "5000",
		"paidSubStartRateLimitPerMin":  "3",
		testPaidSubCurrencyKey:         "RUB",
		testPaidSubStarsEnabledKey:     "false",
		testPaidSubYooKassaEnabledKey:  "false",
		testPaidSubYooKassaTokenKey:    "",
		testPaidSubStripeEnabledKey:    "false",
		testPaidSubStripeTokenKey:      "",
		testPaidSubPayMasterEnabledKey: "false",
		testPaidSubPayMasterTokenKey:   "",
		testPaidSubCryptoBotEnabledKey: "false",
		testPaidSubCryptoBotTokenKey:   "",
		testPaidSubExternalEnabledKey:  "false",
		"paidSubExternalUrlTemplate":   "",
		"paidSubOrderTTLMinutes":       "30",
		"paidSubGreeting":              "",
		testPaidSubRefundRevokeKey:     "true",
	}
}

func testPaidSubBooleanKeys() map[string]struct{} {
	return settingKeySet(
		testPaidSubEnabledKey,
		testPaidSubAutoRegisterKey,
		testPaidSubStarsEnabledKey,
		testPaidSubYooKassaEnabledKey,
		testPaidSubStripeEnabledKey,
		testPaidSubPayMasterEnabledKey,
		testPaidSubCryptoBotEnabledKey,
		testPaidSubExternalEnabledKey,
		testPaidSubRefundRevokeKey,
	)
}

func testPaidSubEncryptedKeys() map[string]struct{} {
	return settingKeySet(
		testPaidSubBotTokenKey,
		testPaidSubYooKassaTokenKey,
		testPaidSubStripeTokenKey,
		testPaidSubPayMasterTokenKey,
		testPaidSubCryptoBotTokenKey,
		testPaidSubProxyURLKey,
		testPaidSubProxyUsernameKey,
		testPaidSubProxyPasswordKey,
	)
}
