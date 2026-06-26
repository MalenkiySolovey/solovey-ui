package service

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"gorm.io/gorm"
)

func TestConfigSaveRollsBackOnPanic(t *testing.T) {
	initSettingTestDB(t)
	before := model.Setting{Key: "webPort", Value: "2095"}
	if err := dbsqlite.DB().Create(&before).Error; err != nil {
		t.Fatal(err)
	}

	const callbackName = "test:panic-before-config-change"
	if err := dbsqlite.DB().Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "changes" {
			panic("injected config-save panic")
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.DB().Callback().Create().Remove(callbackName) })

	payload, err := json.Marshal(map[string]string{"webPort": "23456"})
	if err != nil {
		t.Fatal(err)
	}
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = (&ConfigService{}).Save("settings", "set", payload, "", "admin", "example.com")
	}()
	if recovered == nil {
		t.Fatal("expected injected panic to propagate")
	}

	var after model.Setting
	if err := dbsqlite.DB().Where("key = ?", "webPort").First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after.Value != before.Value {
		t.Fatalf("panicking save committed webPort: got %q want %q", after.Value, before.Value)
	}
	var changes int64
	if err := dbsqlite.DB().Model(&model.Changes{}).Where("key = ?", "settings").Count(&changes).Error; err != nil {
		t.Fatal(err)
	}
	if changes != 0 {
		t.Fatalf("panicking save committed %d change rows", changes)
	}
}
