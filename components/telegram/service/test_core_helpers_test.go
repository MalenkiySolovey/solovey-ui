//go:build !minimal

package telegram_test

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"

	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
)

func initSettingTestDB(t *testing.T) *coreservice.SettingService {
	t.Helper()
	registerTelegramSettingsForTest(t)
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	if err := dbsqlite.Init(filepath.Join(t.TempDir(), "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// The async audit owner must finish before its fixture DB is retired;
		// direct pool closure lets an old batch enter the next fixture's schema.
		if err := coreservice.StopAuditWriter(ctx); err != nil {
			t.Error("drain fixture audit writer", err)
		}
		if err := dbsqlite.Close(); err != nil {
			t.Error("retire fixture database", err)
		}
	})
	return &coreservice.SettingService{}
}

func registerTelegramSettingsForTest(t *testing.T) {
	t.Helper()
	unregister := coreservice.RegisterSettingContribution("test.telegram."+t.Name(), coreservice.SettingContribution{
		Defaults:                telegramsettings.Defaults(),
		Encrypted:               telegramsettings.EncryptedKeys(),
		ClearableEmptyEncrypted: telegramsettings.ClearableEmptyEncryptedKeys(),
		Fields:                  []settingsschema.Field{},
		Validators: []coreservice.SettingValidator{
			telegramsettings.Validate,
		},
	})
	t.Cleanup(unregister)
}

func encodedTestSecretboxKey() string {
	return base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
}

func flushAuditForTest(t testing.TB) {
	t.Helper()
	if err := coreservice.StopAuditWriter(context.Background()); err != nil {
		t.Fatal(err)
	}
}
