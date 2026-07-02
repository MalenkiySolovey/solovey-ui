//go:build !minimal

package telegram_test

import (
	"context"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"
	settingsvalidation "github.com/MalenkiySolovey/solovey-ui/internal/settings/validation"
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
	testDB := dbsqlite.DB()
	t.Cleanup(func() {
		if testDB == nil {
			return
		}
		if sqlDB, err := testDB.DB(); err == nil {
			_ = sqlDB.Close()
			time.Sleep(25 * time.Millisecond)
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
			validateTelegramSettingForTest,
		},
	})
	t.Cleanup(unregister)
}

func validateTelegramSettingForTest(key string, value string, storedSecretMarker string) error {
	if _, ok := telegramsettings.BooleanKeys()[key]; ok {
		_, err := strconv.ParseBool(value)
		return err
	}
	switch key {
	case telegramsettings.BackupEnabledKey:
		if value != "true" && value != "false" {
			return errors.New("invalid boolean setting")
		}
	case telegramsettings.CPUThresholdKey:
		threshold, err := strconv.Atoi(value)
		if err != nil || threshold <= 0 || threshold > 100 {
			return errors.New("invalid cpu threshold setting")
		}
	case telegramsettings.BackupPassphraseKey:
		if value != "" && value != storedSecretMarker && len([]rune(value)) < 12 {
			return errors.New("weak_passphrase")
		}
	case telegramsettings.ProxyURLKey:
		return settingsvalidation.ValidateProxyURLValue(value, storedSecretMarker)
	}
	return nil
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
