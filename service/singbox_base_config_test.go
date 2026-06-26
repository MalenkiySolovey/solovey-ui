package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestSingBoxBaseConfigStoreSetValidatesAndNormalizesConfig(t *testing.T) {
	settingService := initSettingTestDB(t)
	store := NewSingBoxBaseConfigStore(settingService)

	if err := store.Set(`{"dns":{"servers":[]},"route":{"rules":[]}}`); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Get()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved, "\n  \"dns\"") || !strings.Contains(saved, "\n  \"route\"") {
		t.Fatalf("set config was not normalized: %s", saved)
	}

	if err := store.Set(`{"dns":{"servers":{}}}`); err == nil {
		t.Fatal("expected invalid config to be rejected")
	}
}

func TestSingBoxBaseConfigStoreSaveCreatesMissingConfigSetting(t *testing.T) {
	settingService := initSettingTestDB(t)
	tx := dbsqlite.DB().Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}

	config := json.RawMessage(`{"dns":{"servers":[{"tag":"dns-umbrella"}]},"route":{"rules":[{"action":"sniff"}]}}`)
	if err := NewSingBoxBaseConfigStore(settingService).Save(tx, config); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}

	var saved string
	if err := dbsqlite.DB().Model(&model.Setting{}).Select("value").Where("key = ?", "config").Scan(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved, `"dns"`) || !strings.Contains(saved, `"route"`) {
		t.Fatalf("saved config does not contain DNS and route data: %s", saved)
	}
}
