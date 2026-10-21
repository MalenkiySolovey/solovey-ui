package service

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"
)

func TestComponentSettingsDropUsesDurableOwnershipAndDeniesUnknownOwner(t *testing.T) {
	initSettingTestDB(t)
	item := manifest.Manifest{ID: "settings-drop-fixture", Name: "Settings drop fixture", Version: "1", Delivery: manifest.DeliveryInProcess,
		Database: manifest.Database{Settings: []string{"dropFixtureSetting"}, Secrets: []string{"dropFixtureSecret"}}}
	durableowner.Register(item)
	db := dbsqlite.DB()
	for _, key := range []string{"dropFixtureSetting", "dropFixtureSecret", "otherOwnerSetting"} {
		if err := db.Create(&model.Setting{Key: key, Value: "fixture"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	s := &SettingService{}
	if err := s.DropOwnedComponentSettings(t.Context(), "unknown-settings-owner"); err == nil {
		t.Fatal("unknown owner deletion admitted")
	}
	var count int64
	if err := db.Model(&model.Setting{}).Where("key IN ?", []string{"dropFixtureSetting", "dropFixtureSecret", "otherOwnerSetting"}).Count(&count).Error; err != nil || count != 3 {
		t.Fatal("denied drop mutated settings", err)
	}
	// No runtime SettingContribution is registered: stopped owners still have
	// the same durable deletion authority, including encrypted setting keys.
	if err := s.DropOwnedComponentSettings(t.Context(), item.ID); err != nil {
		t.Fatal(err)
	}
	var remaining []model.Setting
	if err := db.Where("key IN ?", []string{"dropFixtureSetting", "dropFixtureSecret", "otherOwnerSetting"}).Find(&remaining).Error; err != nil || len(remaining) != 1 || remaining[0].Key != "otherOwnerSetting" {
		t.Fatal("owner settings deletion crossed scope", err)
	}
}
