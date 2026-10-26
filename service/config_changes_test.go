package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	"gorm.io/gorm"
)

func TestConfigSaveRejectedChangeRollsBackSettingsMaster(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", encodedTestSecretboxKey())
	initSettingTestDB(t)
	replaceDefaultRuntimeForTest(t, NewRuntimeWithCoreProvider(nil))
	db := dbsqlite.DB()
	var before int64
	if err := db.Model(&model.Setting{}).Where("key = ?", settingcatalog.SecretKey).Count(&before).Error; err != nil || before != 0 {
		t.Fatal("fixture must start without a settings master", err)
	}
	rejected := errors.New("fixture change rejected")
	const callback = "test-reject-config-change"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "changes" {
			_ = tx.AddError(rejected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	payload, _ := json.Marshal(map[string]string{"fixturePrimaryBotToken": "1234567890:" + strings.Repeat("A", 35)})
	if _, err := (&ConfigService{}).Save("settings", "set", payload, "", "admin", "example.com"); !errors.Is(err, rejected) {
		t.Fatal("settings transaction did not reach the rejected change", err)
	}
	var after int64
	if err := db.Model(&model.Setting{}).Where("key IN ?", []string{settingcatalog.SecretKey, "fixturePrimaryBotToken"}).Count(&after).Error; err != nil || after != 0 {
		t.Fatal("rejected settings change committed its master or ciphertext", err)
	}
}

func TestConfigSaveRedactsSensitiveChangePayload(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", encodedTestSecretboxKey())
	initSettingTestDB(t)
	replaceDefaultRuntimeForTest(t, NewRuntimeWithCoreProvider(nil))
	payload, err := json.Marshal(map[string]string{
		"fixturePrimaryBotToken": "1234567890:" + strings.Repeat("A", 35),
		"fixturePrimaryChatID":   "42",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := (&ConfigService{}).Save("settings", "set", payload, "", "admin", "example.com"); err != nil {
		t.Fatal(err)
	}

	var change model.Changes
	if err := dbsqlite.DB().Where("key = ?", "settings").First(&change).Error; err != nil {
		t.Fatal(err)
	}
	stored := string(change.Obj)
	if strings.Contains(stored, "1234567890:") || strings.Contains(stored, strings.Repeat("A", 35)) {
		t.Fatalf("change payload leaked secret: %s", stored)
	}
	if !strings.Contains(stored, `"fixturePrimaryBotToken":"[REDACTED]"`) {
		t.Fatalf("change payload was not redacted: %s", stored)
	}
	if !strings.Contains(stored, `"fixturePrimaryChatID":"42"`) {
		t.Fatalf("non-sensitive setting was unexpectedly removed: %s", stored)
	}
}
