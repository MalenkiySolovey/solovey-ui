//go:build !minimal

package telegram_test

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
)

func TestSecurityTelegramBackupAuditOmitsPayloadPassphraseAndToken(t *testing.T) {
	passphrase := "correct horse battery staple"
	settingService := initSettingTestDB(t)
	configureTelegramBackupSettings(t, settingService, telegramBackupSettings{
		TelegramEnabled: true,
		BackupEnabled:   true,
		Passphrase:      passphrase,
	})
	backupService := &telegramservice.TelegramBackupService{
		Settings: testTelegramSettings{},
		Audit:    testTelegramBackupAudit,
		SendDocumentStream: func(_ context.Context, _ string, _ io.Reader, _ string) telegramservice.Result {
			return telegramservice.Result{Success: true}
		},
	}

	result := backupService.RunOnce(telegramservice.ContextWithTelegramBackupActor(context.Background(), "admin"), telegramservice.TelegramBackupTriggerManual)
	if !result.Success {
		t.Fatalf("backup failed: %#v", result)
	}
	flushAuditForTest(t)
	var event model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "tg_backup_sent").Order("id desc").First(&event).Error; err != nil {
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
	if _, err := (&coreservice.ConfigService{}).Save("settings", "set", payload, "", "admin", "example.com"); err != nil {
		t.Fatal(err)
	}
	var change model.Changes
	if err := dbsqlite.DB().Where("key = ?", "settings").Order("id desc").First(&change).Error; err != nil {
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
