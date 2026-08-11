package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	settingsvalidation "github.com/MalenkiySolovey/solovey-ui/internal/settings/validation"
)

func TestSettingContributionStaleCleanupPreservesNewRegistration(t *testing.T) {
	const name = "test.cleanup-generation"
	cleanupOld := RegisterSettingContribution(name, SettingContribution{Defaults: map[string]string{"old": "1"}})
	cleanupOld()
	cleanupNew := RegisterSettingContribution(name, SettingContribution{Defaults: map[string]string{"new": "1"}})
	t.Cleanup(cleanupNew)

	cleanupOld()
	found := false
	for _, entry := range currentSettingContributionEntries() {
		if entry.name == name {
			found = true
			if entry.contribution.Defaults["new"] != "1" {
				t.Fatalf("new registration changed: %#v", entry.contribution.Defaults)
			}
		}
	}
	if !found {
		t.Fatal("stale cleanup removed the new registration")
	}
}

func TestSettingImportAliasesFollowOwnerLifecycle(t *testing.T) {
	cleanup := RegisterSettingContribution("test.import-alias-owner", SettingContribution{
		Defaults:      map[string]string{"fixtureCanonicalSetting": ""},
		ImportAliases: map[string]string{"legacyFixtureSetting": "fixtureCanonicalSetting"},
	})
	if got := CurrentSettingImportAliases()["legacyFixtureSetting"]; got != "fixtureCanonicalSetting" {
		t.Fatalf("import alias target = %q", got)
	}
	cleanup()
	if _, exists := CurrentSettingImportAliases()["legacyFixtureSetting"]; exists {
		t.Fatal("owner cleanup left its import alias registered")
	}
}

func TestSettingImportAliasesRejectUnownedTargetsAndDuplicateAuthority(t *testing.T) {
	assertPanics := func(name string, register func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s did not panic", name)
			}
		}()
		register()
	}
	assertPanics("unowned target", func() {
		RegisterSettingContribution("test.import-alias-unowned", SettingContribution{
			ImportAliases: map[string]string{"legacyUnowned": "unownedCanonical"},
		})
	})

	cleanup := RegisterSettingContribution("test.import-alias-first", SettingContribution{
		Defaults:      map[string]string{"firstCanonical": ""},
		ImportAliases: map[string]string{"legacyCollision": "firstCanonical"},
	})
	t.Cleanup(cleanup)
	assertPanics("duplicate authority", func() {
		RegisterSettingContribution("test.import-alias-second", SettingContribution{
			Defaults:      map[string]string{"secondCanonical": ""},
			ImportAliases: map[string]string{"legacyCollision": "secondCanonical"},
		})
	})
	assertPanics("alias conflicts with core setting", func() {
		RegisterSettingContribution("test.import-alias-core", SettingContribution{
			Defaults:      map[string]string{"fixtureAliasCoreTarget": ""},
			ImportAliases: map[string]string{"timeLocation": "fixtureAliasCoreTarget"},
		})
	})
	assertPanics("alias conflicts with component setting", func() {
		RegisterSettingContribution("test.import-alias-component-key", SettingContribution{
			Defaults:      map[string]string{"fixtureAliasComponentTarget": ""},
			ImportAliases: map[string]string{"firstCanonical": "fixtureAliasComponentTarget"},
		})
	})
	assertPanics("setting conflicts with component alias", func() {
		RegisterSettingContribution("test.import-alias-component-target", SettingContribution{
			Defaults: map[string]string{"legacyCollision": ""},
		})
	})
}

func TestSettingContributionsRejectDuplicateAndCoreSettingAuthority(t *testing.T) {
	assertPanics := func(name string, register func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s did not panic", name)
			}
		}()
		register()
	}

	cleanup := RegisterSettingContribution("test.owner-first", SettingContribution{
		Defaults: map[string]string{"fixtureOwnedSetting": "first"},
	})
	t.Cleanup(cleanup)
	assertPanics("duplicate component authority", func() {
		RegisterSettingContribution("test.owner-second", SettingContribution{
			Defaults: map[string]string{"fixtureOwnedSetting": "second"},
		})
	})
	assertPanics("core authority", func() {
		RegisterSettingContribution("test.owner-core", SettingContribution{
			Defaults: map[string]string{"timeLocation": "UTC"},
		})
	})
	assertPanics("orphan metadata", func() {
		RegisterSettingContribution("test.owner-orphan", SettingContribution{
			Encrypted: map[string]struct{}{"fixtureMissingDefault": {}},
		})
	})
}

func TestSettingContributionRegistryBoundsCardinality(t *testing.T) {
	settingContributions.RLock()
	baseline := len(settingContributions.entries)
	settingContributions.RUnlock()

	cleanups := make([]func(), 0, maxSettingContributions-baseline)
	defer func() {
		for index := len(cleanups) - 1; index >= 0; index-- {
			cleanups[index]()
		}
	}()
	for index := baseline; index < maxSettingContributions; index++ {
		cleanups = append(cleanups, RegisterSettingContribution(
			fmt.Sprintf("test.capacity.%03d", index),
			SettingContribution{},
		))
	}

	defer func() {
		if recover() == nil {
			t.Fatal("registry accepted a contribution past its cardinality bound")
		}
	}()
	RegisterSettingContribution("test.capacity.overflow", SettingContribution{})
}

const (
	testFixturePrimaryEnabledKey             = "fixturePrimaryEnabled"
	testFixturePrimaryBotTokenKey            = "fixturePrimaryBotToken"
	testFixturePrimaryProxyURLKey            = "fixturePrimaryProxyURL"
	testFixturePrimaryProxyUsernameKey       = "fixturePrimaryProxyUsername"
	testFixturePrimaryProxyPasswordKey       = "fixturePrimaryProxyPassword"
	testFixturePrimaryTransportModeKey       = "fixturePrimaryTransportMode"
	testFixturePrimaryOutboundTagKey         = "fixturePrimaryOutboundTag"
	testFixturePrimaryCPUThresholdKey        = "fixturePrimaryCPUThreshold"
	testFixturePrimaryNotifyCPUKey           = "fixturePrimaryNotifyCPU"
	testFixturePrimaryReportKey              = "fixturePrimaryReport"
	testFixturePrimaryReportCronKey          = "fixturePrimaryReportCron"
	testFixturePrimaryBackupEnabledKey       = "fixturePrimaryBackupEnabled"
	testFixturePrimaryBackupPassphraseKey    = "fixturePrimaryBackupPassphrase"
	testFixturePrimaryBackupCronKey          = "fixturePrimaryBackupCron"
	testFixturePrimaryBackupExcludeTablesKey = "fixturePrimaryBackupExcludeTables"
	testFixturePrimaryBackupMaxSizeMBKey     = "fixturePrimaryBackupMaxSizeMB"
	testFixtureSecondaryEnabledKey           = "fixtureSecondaryEnabled"
	testFixtureSecondaryBotTokenKey          = "fixtureSecondaryBotToken"
	testFixtureSecondaryBotPollSecondsKey    = "fixtureSecondaryPollSeconds"
	testFixtureSecondaryUpdateOffsetKey      = "fixtureSecondaryUpdateOffset"
	testFixtureSecondaryProxyURLKey          = "fixtureSecondaryProxyURL"
	testFixtureSecondaryProxyUsernameKey     = "fixtureSecondaryProxyUsername"
	testFixtureSecondaryProxyPasswordKey     = "fixtureSecondaryProxyPassword"
	testFixtureSecondaryAutoRegisterKey      = "fixtureSecondaryAutoRegister"
	testFixtureSecondaryAutoInboundsKey      = "fixtureSecondaryAutoItems"
	testFixtureSecondaryStarsEnabledKey      = "fixtureSecondaryNativeEnabled"
	testFixtureSecondaryYooKassaEnabledKey   = "fixtureSecondaryProviderAEnabled"
	testFixtureSecondaryYooKassaTokenKey     = "fixtureSecondaryProviderAToken"
	testFixtureSecondaryStripeEnabledKey     = "fixtureSecondaryProviderDEnabled"
	testFixtureSecondaryStripeTokenKey       = "fixtureSecondaryProviderDToken"
	testFixtureSecondaryPayMasterEnabledKey  = "fixtureSecondaryProviderBEnabled"
	testFixtureSecondaryPayMasterTokenKey    = "fixtureSecondaryProviderBToken"
	testFixtureSecondaryCryptoBotEnabledKey  = "fixtureSecondaryProviderCEnabled"
	testFixtureSecondaryCryptoBotTokenKey    = "fixtureSecondaryProviderCToken"
	testFixtureSecondaryExternalEnabledKey   = "fixtureSecondaryExternalEnabled"
	testFixtureSecondaryRefundRevokeKey      = "fixtureSecondaryRefundRevoke"
	testFixtureSecondaryCurrencyKey          = "fixtureSecondaryCurrency"
)

func registerFixturePrimarySettingsForTest(t *testing.T) {
	t.Helper()
	if _, ok := currentSettingsSchema().Default(testFixturePrimaryEnabledKey); ok {
		return
	}
	unregister := RegisterSettingContribution("test.fixture-primary", SettingContribution{
		Defaults:                testFixturePrimaryDefaults(),
		Encrypted:               testFixturePrimaryEncryptedKeys(),
		ClearableEmptyEncrypted: settingKeySet(testFixturePrimaryBackupPassphraseKey),
		Validators: []SettingValidator{
			validateFixturePrimarySettingForTest,
		},
	})
	t.Cleanup(unregister)
}

func registerFixtureSecondarySettingsForTest(t *testing.T) {
	t.Helper()
	if _, ok := currentSettingsSchema().Default(testFixtureSecondaryEnabledKey); ok {
		return
	}
	unregister := RegisterSettingContribution("test.fixture-secondary", SettingContribution{
		Defaults:  testFixtureSecondaryDefaults(),
		Internal:  settingKeySet(testFixtureSecondaryUpdateOffsetKey),
		Encrypted: testFixtureSecondaryEncryptedKeys(),
		Validators: []SettingValidator{
			validateFixtureSecondarySettingForTest,
		},
	})
	t.Cleanup(unregister)
}

func registerFixtureSettingContributionsForTest(t *testing.T) {
	t.Helper()
	registerFixturePrimarySettingsForTest(t)
	registerFixtureSecondarySettingsForTest(t)
}

func validateFixturePrimarySettingForTest(key string, value string, storedSecretMarker string) error {
	if _, ok := testFixturePrimaryBooleanKeys()[key]; ok {
		_, err := strconv.ParseBool(value)
		return err
	}
	switch key {
	case testFixturePrimaryCPUThresholdKey:
		threshold, err := strconv.Atoi(value)
		if err != nil || threshold <= 0 || threshold > 100 {
			return errors.New("invalid cpu threshold setting")
		}
	case testFixturePrimaryBackupPassphraseKey:
		if value != "" && value != storedSecretMarker && len([]rune(value)) < 12 {
			return errors.New("weak_passphrase")
		}
	case testFixturePrimaryProxyURLKey:
		return settingsvalidation.ValidateProxyURLValue(value, storedSecretMarker)
	}
	return nil
}

func testFixturePrimaryDefaults() map[string]string {
	return map[string]string{
		testFixturePrimaryEnabledKey:             "false",
		testFixturePrimaryBotTokenKey:            "",
		"fixturePrimaryChatID":                   "",
		testFixturePrimaryProxyURLKey:            "",
		testFixturePrimaryProxyUsernameKey:       "",
		testFixturePrimaryProxyPasswordKey:       "",
		testFixturePrimaryTransportModeKey:       "proxy",
		testFixturePrimaryOutboundTagKey:         "",
		testFixturePrimaryCPUThresholdKey:        "90",
		testFixturePrimaryNotifyCPUKey:           "false",
		testFixturePrimaryReportKey:              "false",
		testFixturePrimaryReportCronKey:          "",
		testFixturePrimaryBackupEnabledKey:       "false",
		testFixturePrimaryBackupPassphraseKey:    "",
		testFixturePrimaryBackupCronKey:          "",
		testFixturePrimaryBackupExcludeTablesKey: "stats,client_ips,audit_events,changes",
		testFixturePrimaryBackupMaxSizeMBKey:     "45",
	}
}

func testFixturePrimaryBooleanKeys() map[string]struct{} {
	return settingKeySet(
		testFixturePrimaryEnabledKey,
		testFixturePrimaryNotifyCPUKey,
		testFixturePrimaryReportKey,
		testFixturePrimaryBackupEnabledKey,
	)
}

func testFixturePrimaryEncryptedKeys() map[string]struct{} {
	return settingKeySet(
		testFixturePrimaryBackupPassphraseKey,
		testFixturePrimaryBotTokenKey,
		testFixturePrimaryProxyPasswordKey,
		testFixturePrimaryProxyURLKey,
		testFixturePrimaryProxyUsernameKey,
	)
}

func validateFixtureSecondarySettingForTest(key string, value string, storedSecretMarker string) error {
	if _, ok := testFixtureSecondaryBooleanKeys()[key]; ok {
		_, err := strconv.ParseBool(value)
		return err
	}
	switch key {
	case testFixtureSecondaryAutoInboundsKey:
		if value == "" {
			return nil
		}
		var ids []uint
		return json.Unmarshal([]byte(value), &ids)
	case testFixtureSecondaryProxyURLKey:
		return settingsvalidation.ValidateProxyURLValue(value, storedSecretMarker)
	case testFixtureSecondaryCurrencyKey:
		if len(strings.ToUpper(strings.TrimSpace(value))) != 3 {
			return errors.New("fixtureSecondaryCurrency must be a 3-letter code")
		}
	}
	return nil
}

func testFixtureSecondaryDefaults() map[string]string {
	return map[string]string{
		testFixtureSecondaryEnabledKey:          "false",
		testFixtureSecondaryBotTokenKey:         "",
		testFixtureSecondaryBotPollSecondsKey:   "25",
		testFixtureSecondaryUpdateOffsetKey:     "0",
		"fixtureSecondaryTransportMode":         "proxy",
		testFixtureSecondaryProxyURLKey:         "",
		testFixtureSecondaryProxyUsernameKey:    "",
		testFixtureSecondaryProxyPasswordKey:    "",
		"fixtureSecondaryOutboundTag":           "",
		testFixtureSecondaryAutoRegisterKey:     "false",
		testFixtureSecondaryAutoInboundsKey:     "[]",
		"fixtureSecondaryTrialDays":             "3",
		"fixtureSecondaryTrialVolume":           "0",
		"fixtureSecondaryMaxItems":              "5000",
		"fixtureSecondaryStartRateLimitPerMin":  "3",
		testFixtureSecondaryCurrencyKey:         "RUB",
		testFixtureSecondaryStarsEnabledKey:     "false",
		testFixtureSecondaryYooKassaEnabledKey:  "false",
		testFixtureSecondaryYooKassaTokenKey:    "",
		testFixtureSecondaryStripeEnabledKey:    "false",
		testFixtureSecondaryStripeTokenKey:      "",
		testFixtureSecondaryPayMasterEnabledKey: "false",
		testFixtureSecondaryPayMasterTokenKey:   "",
		testFixtureSecondaryCryptoBotEnabledKey: "false",
		testFixtureSecondaryCryptoBotTokenKey:   "",
		testFixtureSecondaryExternalEnabledKey:  "false",
		"fixtureSecondaryExternalURLTemplate":   "",
		"fixtureSecondaryOrderTTLMinutes":       "30",
		"fixtureSecondaryGreeting":              "",
		testFixtureSecondaryRefundRevokeKey:     "true",
	}
}

func testFixtureSecondaryBooleanKeys() map[string]struct{} {
	return settingKeySet(
		testFixtureSecondaryEnabledKey,
		testFixtureSecondaryAutoRegisterKey,
		testFixtureSecondaryStarsEnabledKey,
		testFixtureSecondaryYooKassaEnabledKey,
		testFixtureSecondaryStripeEnabledKey,
		testFixtureSecondaryPayMasterEnabledKey,
		testFixtureSecondaryCryptoBotEnabledKey,
		testFixtureSecondaryExternalEnabledKey,
		testFixtureSecondaryRefundRevokeKey,
	)
}

func testFixtureSecondaryEncryptedKeys() map[string]struct{} {
	return settingKeySet(
		testFixtureSecondaryBotTokenKey,
		testFixtureSecondaryYooKassaTokenKey,
		testFixtureSecondaryStripeTokenKey,
		testFixtureSecondaryPayMasterTokenKey,
		testFixtureSecondaryCryptoBotTokenKey,
		testFixtureSecondaryProxyURLKey,
		testFixtureSecondaryProxyUsernameKey,
		testFixtureSecondaryProxyPasswordKey,
	)
}
