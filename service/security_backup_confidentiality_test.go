package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/deposist/s-ui-x/database"
	"github.com/deposist/s-ui-x/database/model"
)

func TestSecurityTelegramBackupAuditOmitsPayloadPassphraseAndToken(t *testing.T) {
	passphrase := "correct horse battery staple"
	settingService := initSettingTestDB(t)
	configureTelegramBackupSettings(t, settingService, telegramBackupSettings{
		TelegramEnabled: true,
		BackupEnabled:   true,
		Passphrase:      passphrase,
	})
	restoreSend := replaceTelegramBackupSendDocumentForTest(t, func(_ *TelegramService, _ string, _ []byte, _ string) TelegramResult {
		return TelegramResult{Success: true}
	})
	defer restoreSend()

	result := (&TelegramBackupService{}).RunOnce(ContextWithTelegramBackupActor(context.Background(), "admin"), TelegramBackupTriggerManual)
	if !result.Success {
		t.Fatalf("backup failed: %#v", result)
	}
	var event model.AuditEvent
	if err := database.GetDB().Where("event = ?", "tg_backup_sent").Order("id desc").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	details := string(event.Details)
	for _, forbidden := range []string{
		passphrase,
		"123456:test-token",
		"SQLite format 3",
	} {
		if strings.Contains(details, forbidden) {
			t.Fatalf("backup audit leaked %q in details: %s", forbidden, details)
		}
	}
	for _, expected := range []string{`"payloadSizeBytes"`, `"envelopeSizeBytes"`, `"channel":"telegram"`} {
		if !strings.Contains(details, expected) {
			t.Fatalf("backup audit missing %s in details: %s", expected, details)
		}
	}
}

func TestSecurityConfigChangeRedactsTelegramBackupPassphrase(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", encodedTestSecretboxKey())
	initSettingTestDB(t)
	passphrase := "correct horse battery staple"
	payload, err := json.Marshal(map[string]string{
		"telegramBackupPassphrase": passphrase,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&ConfigService{}).Save("settings", "set", payload, "", "admin", "example.com"); err != nil {
		t.Fatal(err)
	}
	var change model.Changes
	if err := database.GetDB().Where("key = ?", "settings").Order("id desc").First(&change).Error; err != nil {
		t.Fatal(err)
	}
	stored := string(change.Obj)
	if strings.Contains(stored, passphrase) {
		t.Fatalf("change payload leaked telegramBackupPassphrase: %s", stored)
	}
	if !strings.Contains(stored, `"telegramBackupPassphrase":"[REDACTED]"`) {
		t.Fatalf("change payload did not redact telegramBackupPassphrase: %s", stored)
	}
}

func TestSecurityTelegramBackupZeroizationOnError_XFAILPhase4(t *testing.T) {
	t.Skip("XFAIL Phase4: RunOnce zeroization is implemented with defers, but no production hook exposes buffers for deterministic post-error assertion")
}
