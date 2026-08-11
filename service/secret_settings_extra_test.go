package service

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
)

func TestEncryptDecryptSettingValueRoundTripExtra(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", encodedTestSecretboxKey())
	settingService := initSettingTestDB(t)

	encrypted, err := settingService.encryptSettingValue("fixturePrimaryBotToken", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "test-secret" || !secretbox.IsEncrypted(encrypted) {
		t.Fatalf("value was not encrypted: %q", encrypted)
	}
	decrypted, err := settingService.decryptSettingValue("fixturePrimaryBotToken", encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "test-secret" {
		t.Fatalf("unexpected decrypted value %q", decrypted)
	}
}

func TestDecryptPrimarySecretboxCandidateDoesNotAuditFallback(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", encodedTestSecretboxKey())
	settingService := initSettingTestDB(t)

	encrypted, err := settingService.encryptSettingValue("fixturePrimaryProxyPassword", "primary-secret")
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := settingService.decryptSettingValue("fixturePrimaryProxyPassword", encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "primary-secret" {
		t.Fatalf("unexpected decrypted value %q", decrypted)
	}

	flushAuditForTest(t)
	var count int64
	if err := dbsqlite.DB().Model(model.AuditEvent{}).Where("event = ?", "settings_secretbox_key_fallback").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("primary candidate decrypt wrote fallback audit events: %d", count)
	}
}

func TestLegacySecretboxFallbackDoesNotAuditAfterFix_XFAILIssue17(t *testing.T) {
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
	if _, err := settingService.decryptSettingValue("fixturePrimaryBotToken", legacyValue); err != nil {
		t.Fatal(err)
	}

	flushAuditForTest(t)
	var count int64
	if err := dbsqlite.DB().Model(model.AuditEvent{}).Where("event = ?", "settings_secretbox_key_fallback").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("legacy fallback should not write audit noise after issue 17 fix, got %d", count)
	}
}
