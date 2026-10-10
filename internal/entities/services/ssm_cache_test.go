package services

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/ssmcache"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSSMCacheSaveRejectsUnownedAndSharedPathsBeforeMutation(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "services.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err = db.AutoMigrate(&model.Service{}, &model.Tls{}); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(ssmcache.Root(), "state.json")
	options, _ := json.Marshal(map[string]any{"cache_path": name, "servers": map[string]string{"/main": "managed"}})
	row := model.Service{Tag: "first", Type: "ssm-api", Options: options}
	if err = db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	for _, cache := range []string{filepath.Join(t.TempDir(), "unowned"), filepath.Join(ssmcache.Root(), "sub", "..", "state.json")} {
		raw, _ := json.Marshal(map[string]any{"type": "ssm-api", "tag": "second", "cache_path": cache, "servers": map[string]string{"/main": "managed"}})
		if _, err = Save(db, "new", raw); err == nil {
			t.Fatal("unowned/shared service accepted")
		}
		var count int64
		if err = db.Model(&model.Service{}).Count(&count).Error; err != nil || count != 1 {
			t.Fatal("rejected save changed service inventory")
		}
		var after model.Service
		if err = db.First(&after, row.Id).Error; err != nil || !bytes.Equal(after.Options, options) {
			t.Fatal("rejected save changed existing state")
		}
	}
}
