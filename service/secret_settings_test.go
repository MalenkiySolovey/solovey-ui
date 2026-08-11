package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

func initSettingTestDB(t *testing.T) *SettingService {
	t.Helper()
	registerFixtureSettingContributionsForTest(t)
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	if err := dbsqlite.Init(filepath.Join(t.TempDir(), "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	testDB := dbsqlite.DB()
	t.Cleanup(func() {
		if testDB != nil {
			if sqlDB, err := testDB.DB(); err == nil {
				_ = sqlDB.Close()
				time.Sleep(25 * time.Millisecond)
			}
		}
	})
	return &SettingService{}
}

func encodedTestSecretboxKey() string {
	return base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
}

func TestSecretSettingIsEncryptedAndMasked(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", encodedTestSecretboxKey())
	settingService := initSettingTestDB(t)

	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(map[string]string{
		"fixturePrimaryBotToken": "123456:secret-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		return settingService.Save(tx, payload)
	}); err != nil {
		t.Fatal(err)
	}

	var setting model.Setting
	if err := dbsqlite.DB().Where("key = ?", "fixturePrimaryBotToken").First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	if setting.Value == "123456:secret-token" || !secretbox.IsEncrypted(setting.Value) {
		t.Fatalf("secret setting was not encrypted: %q", setting.Value)
	}

	decrypted, err := settingService.getString("fixturePrimaryBotToken")
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "123456:secret-token" {
		t.Fatalf("unexpected decrypted value %q", decrypted)
	}

	settings, err := settingService.GetAllSetting()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := (*settings)["fixturePrimaryBotToken"]; ok {
		t.Fatal("raw fixturePrimaryBotToken leaked through settings API")
	}
	if (*settings)["fixturePrimaryBotTokenHasSecret"] != "true" {
		t.Fatalf("expected has-secret marker, got %q", (*settings)["fixturePrimaryBotTokenHasSecret"])
	}

	emptyPayload, err := json.Marshal(map[string]string{
		"fixturePrimaryBotToken": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		return settingService.Save(tx, emptyPayload)
	}); err != nil {
		t.Fatal(err)
	}
	afterEmpty, err := settingService.getString("fixturePrimaryBotToken")
	if err != nil {
		t.Fatal(err)
	}
	if afterEmpty != "123456:secret-token" {
		t.Fatalf("empty secret save should keep old value, got %q", afterEmpty)
	}
}

func TestLegacyPlaintextSecretRoundTripEncryptsOnSave(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", encodedTestSecretboxKey())
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", "fixturePrimaryProxyPassword").Update("value", "legacy-plain-secret").Error; err != nil {
		t.Fatal(err)
	}

	got, err := settingService.getString("fixturePrimaryProxyPassword")
	if err != nil {
		t.Fatal(err)
	}
	if got != "legacy-plain-secret" {
		t.Fatalf("legacy plaintext secret did not round-trip: %q", got)
	}

	payload, err := json.Marshal(map[string]string{
		"fixturePrimaryProxyPassword": got,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		return settingService.Save(tx, payload)
	}); err != nil {
		t.Fatal(err)
	}

	var stored model.Setting
	if err := dbsqlite.DB().Where("key = ?", "fixturePrimaryProxyPassword").First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Value == got || !secretbox.IsEncrypted(stored.Value) {
		t.Fatalf("legacy plaintext secret was not encrypted on save: %q", stored.Value)
	}
	after, err := settingService.getString("fixturePrimaryProxyPassword")
	if err != nil {
		t.Fatal(err)
	}
	if after != got {
		t.Fatalf("encrypted legacy secret did not round-trip: %q", after)
	}
}

func TestComponentBackupPassphraseEncryptedMaskedAndClearable(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", encodedTestSecretboxKey())
	settingService := initSettingTestDB(t)
	registerFixtureBackupPassphraseAuditObserverForTest(t)

	settings, err := settingService.GetAllSetting()
	if err != nil {
		t.Fatal(err)
	}
	if (*settings)["fixturePrimaryBackupPassphrase"] != "" || (*settings)["fixturePrimaryBackupPassphraseHasSecret"] != "false" {
		t.Fatalf("unexpected default passphrase markers: %#v", *settings)
	}

	weakPayload, err := json.Marshal(map[string]string{
		"fixturePrimaryBackupPassphrase": "too-short",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		return settingService.Save(tx, weakPayload)
	}); err == nil || !strings.Contains(err.Error(), "weak_passphrase") {
		t.Fatalf("expected weak passphrase validation, got %v", err)
	}

	passphrase := "correct horse battery staple"
	payload, err := json.Marshal(map[string]string{
		"fixturePrimaryBackupPassphrase": passphrase,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&ConfigService{}).Save("settings", "set", payload, "", "admin", "localhost"); err != nil {
		t.Fatal(err)
	}
	var stored model.Setting
	if err := dbsqlite.DB().Where("key = ?", "fixturePrimaryBackupPassphrase").First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Value == passphrase || !secretbox.IsEncrypted(stored.Value) {
		t.Fatalf("backup passphrase was not encrypted: %q", stored.Value)
	}
	decrypted, err := settingService.GetComponentSettingSecretBytes(testFixturePrimaryBackupPassphraseKey)
	if err != nil {
		t.Fatal(err)
	}
	if string(decrypted) != passphrase {
		t.Fatalf("unexpected passphrase %q", string(decrypted))
	}
	common.WipeBytes(decrypted)

	settings, err = settingService.GetAllSetting()
	if err != nil {
		t.Fatal(err)
	}
	if (*settings)["fixturePrimaryBackupPassphrase"] != StoredSecretMarker || (*settings)["fixturePrimaryBackupPassphraseHasSecret"] != "true" {
		t.Fatalf("passphrase was not masked: %#v", *settings)
	}

	flushAuditForTest(t)
	var event model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "fixture_backup_passphrase_changed").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Actor != "admin" || event.Severity != AuditSeverityInfo || strings.Contains(string(event.Details), passphrase) {
		t.Fatalf("unexpected audit event: %#v details=%s", event, event.Details)
	}

	clearPayload, err := json.Marshal(map[string]string{
		"fixturePrimaryBackupPassphrase": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&ConfigService{}).Save("settings", "set", clearPayload, "", "admin", "localhost"); err != nil {
		t.Fatal(err)
	}
	decrypted, err = settingService.GetComponentSettingSecretBytes(testFixturePrimaryBackupPassphraseKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(decrypted) != 0 {
		t.Fatalf("passphrase was not cleared: %q", string(decrypted))
	}
}

func registerFixtureBackupPassphraseAuditObserverForTest(t *testing.T) {
	t.Helper()
	resetConfigSaveObserversForTest()
	unregister := RegisterConfigSaveObserver("test.fixture-primary", func(ctx ConfigSaveObserverContext) (ConfigSaveAfterCommit, error) {
		if ctx.Object != "settings" {
			return nil, nil
		}
		var settings map[string]string
		if err := json.Unmarshal(ctx.Data, &settings); err != nil {
			return nil, err
		}
		newPassphrase, ok := settings[testFixturePrimaryBackupPassphraseKey]
		if !ok || newPassphrase == StoredSecretMarker {
			return nil, nil
		}
		oldPassphrase, err := (&SettingService{}).GetComponentSettingSecretBytes(testFixturePrimaryBackupPassphraseKey)
		if err != nil {
			return nil, err
		}
		defer common.WipeBytes(oldPassphrase)
		if string(oldPassphrase) == newPassphrase {
			return nil, nil
		}
		configured := newPassphrase != ""
		return func() {
			_ = (&AuditService{}).Record(AuditEvent{
				Actor:    ctx.LoginUser,
				Event:    "fixture_backup_passphrase_changed",
				Resource: "database",
				Severity: AuditSeverityInfo,
				Details: map[string]any{
					"configured": configured,
				},
			})
		}, nil
	})
	t.Cleanup(func() {
		unregister()
		resetConfigSaveObserversForTest()
	})
}

func TestGetCookieKeysDerivedFromSecretByDefault(t *testing.T) {
	settingService := initSettingTestDB(t)

	secret, err := settingService.GetSecret()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := settingService.GetCookieKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) == 0 {
		t.Fatalf("expected at least one cookie key, got %d", len(keys))
	}
	if len(keys[0]) != 32 {
		t.Fatalf("expected 32-byte cookie key, got %d", len(keys[0]))
	}
	if bytes.Equal(keys[0], secret) {
		t.Fatal("cookie key must be domain-separated from settings.secret")
	}
	if len(keys) < 2 {
		t.Fatalf("expected legacy cookie fallback key, got %d keys", len(keys))
	}
	keys2, err := settingService.GetCookieKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys2) != len(keys) {
		t.Fatalf("cookie key count changed between calls: %d != %d", len(keys2), len(keys))
	}
	for i := range keys {
		if !bytes.Equal(keys[i], keys2[i]) {
			t.Fatalf("derived cookie key %d changed between calls", i)
		}
	}
}

func TestDerivedSettingKeysUseDomainSeparatedInfo(t *testing.T) {
	master := []byte("test-master-key-material-32-bytes!!")
	cookieKey, err := deriveHKDFKey(master, nil, cookieKeyHKDFInfo)
	if err != nil {
		t.Fatal(err)
	}
	secretboxKey, err := deriveHKDFKey(master, nil, settingsSecretboxKeyHKDFInfo)
	if err != nil {
		t.Fatal(err)
	}
	cookieKeyAgain, err := deriveHKDFKey(master, nil, cookieKeyHKDFInfo)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cookieKey, cookieKeyAgain) {
		t.Fatal("cookie HKDF derivation is not deterministic")
	}
	if bytes.Equal(cookieKey, secretboxKey) {
		t.Fatal("cookie and settings secretbox keys must use distinct HKDF info")
	}
}

func TestGetCookieKeysUsesEnvRolloverList(t *testing.T) {
	settingService := initSettingTestDB(t)

	key1 := []byte("0123456789abcdef0123456789abcdef")
	key2 := []byte("abcdef0123456789abcdef0123456789")
	t.Setenv("SUI_COOKIE_KEY", base64.RawURLEncoding.EncodeToString(key1)+","+base64.RawURLEncoding.EncodeToString(key2))

	keys, err := settingService.GetCookieKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected two cookie keys, got %d", len(keys))
	}
	if !bytes.Equal(keys[0], key1) || !bytes.Equal(keys[1], key2) {
		t.Fatalf("unexpected cookie key order/values: %q %q", keys[0], keys[1])
	}
}

func TestGetCookieKeysInvalidEnvFallsBackToDerivedKey(t *testing.T) {
	t.Setenv("SUI_COOKIE_KEY", "short")
	settingService := initSettingTestDB(t)

	keys, err := settingService.GetCookieKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) < 2 || len(keys[0]) != 32 {
		t.Fatalf("expected derived 32-byte cookie key, got %d keys len=%d", len(keys), len(keys[0]))
	}
	var count int64
	if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", "secret").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected fallback to create secret setting row, got %d", count)
	}
}

func TestInvalidSecretboxEnvFallsBackToSettingsSecret(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", "short")
	settingService := initSettingTestDB(t)

	encrypted, err := settingService.encryptSettingValue("fixturePrimaryBotToken", "fallback-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !secretbox.IsEncrypted(encrypted) {
		t.Fatalf("expected encrypted value, got %q", encrypted)
	}
	decrypted, err := settingService.decryptSettingValue("fixturePrimaryBotToken", encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "fallback-secret" {
		t.Fatalf("unexpected decrypted value %q", decrypted)
	}

	var count int64
	if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", "secret").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected fallback to create secret setting row, got %d", count)
	}
}

func TestSecretboxUsesEnvRawKeyOverride(t *testing.T) {
	rawKey := []byte("abcdef0123456789abcdef0123456789")
	t.Setenv("SUI_SECRETBOX_KEY", base64.RawURLEncoding.EncodeToString(rawKey))
	settingService := initSettingTestDB(t)

	encrypted, err := settingService.encryptSettingValue("fixturePrimaryBotToken", "env-secret")
	if err != nil {
		t.Fatal(err)
	}
	rawBox, err := secretbox.NewRawKey(rawKey)
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := rawBox.DecryptString(encrypted, "fixturePrimaryBotToken")
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "env-secret" {
		t.Fatalf("unexpected decrypted value %q", decrypted)
	}

	legacyBox, err := secretbox.New(rawKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacyBox.DecryptString(encrypted, "fixturePrimaryBotToken"); err == nil {
		t.Fatal("env raw-key ciphertext should not decrypt with legacy HKDF constructor")
	}
}

func TestSecretboxLegacyFallbackAudits(t *testing.T) {
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	secret, err := settingService.GetSecret()
	if err != nil {
		t.Fatal(err)
	}
	legacyBox, err := secretbox.New(secret)
	if err != nil {
		t.Fatal(err)
	}
	legacyValue, err := legacyBox.EncryptString("legacy-secret", "fixturePrimaryBotToken")
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", "fixturePrimaryBotToken").Update("value", legacyValue).Error; err != nil {
		t.Fatal(err)
	}

	got, err := settingService.getString("fixturePrimaryBotToken")
	if err != nil {
		t.Fatal(err)
	}
	if got != "legacy-secret" {
		t.Fatalf("unexpected legacy decrypted value %q", got)
	}

	flushAuditForTest(t)
	var event model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "settings_secretbox_key_fallback").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Resource != "settings" || event.Severity != AuditSeverityWarn {
		t.Fatalf("unexpected fallback audit event: %#v", event)
	}
	if !strings.Contains(string(event.Details), `"key":"fixturePrimaryBotToken"`) ||
		!strings.Contains(string(event.Details), `"candidate":"legacy_settings_secret"`) {
		t.Fatalf("unexpected fallback audit details: %s", event.Details)
	}
	if strings.Contains(string(event.Details), "legacy-secret") {
		t.Fatalf("secret leaked to fallback audit details: %s", event.Details)
	}
}

func TestSecretboxEnvOverrideCanReadSettingsSecretLegacyCiphertext(t *testing.T) {
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	secret, err := settingService.GetSecret()
	if err != nil {
		t.Fatal(err)
	}
	legacyBox, err := secretbox.New(secret)
	if err != nil {
		t.Fatal(err)
	}
	legacyValue, err := legacyBox.EncryptString("legacy-before-env", "fixturePrimaryProxyPassword")
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", "fixturePrimaryProxyPassword").Update("value", legacyValue).Error; err != nil {
		t.Fatal(err)
	}

	t.Setenv("SUI_SECRETBOX_KEY", encodedTestSecretboxKey())
	got, err := settingService.getString("fixturePrimaryProxyPassword")
	if err != nil {
		t.Fatal(err)
	}
	if got != "legacy-before-env" {
		t.Fatalf("unexpected fallback value %q", got)
	}

	flushAuditForTest(t)
	var event model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "settings_secretbox_key_fallback").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(event.Details), `"candidate":"legacy_settings_secret"`) {
		t.Fatalf("unexpected fallback audit details: %s", event.Details)
	}
}
