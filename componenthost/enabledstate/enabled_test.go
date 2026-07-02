package enabledstate

import (
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"gorm.io/gorm"
)

func TestEnabledUsesManifestDefaultWhenSettingMissing(t *testing.T) {
	initEnabledStateTestDB(t)

	enabled, err := Enabled(manifest.Manifest{ID: "telegram", DefaultEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("missing setting should use manifest default")
	}
}

func TestEnabledReadsComponentSetting(t *testing.T) {
	initEnabledStateTestDB(t)
	if err := dbsqlite.DB().Create(&model.Setting{Key: SettingKey("telegram"), Value: "false"}).Error; err != nil {
		t.Fatal(err)
	}

	enabled, err := Enabled(manifest.Manifest{ID: "telegram", DefaultEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("telegram should be disabled by setting")
	}
}

func initEnabledStateTestDB(t *testing.T) {
	t.Helper()
	tempDir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", tempDir)
	closeEnabledStateTestDB(dbsqlite.DB())
	if err := dbsqlite.Init(filepath.Join(tempDir, "s-ui.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeEnabledStateTestDB(dbsqlite.DB())
	})
}

func closeEnabledStateTestDB(db *gorm.DB) {
	if db == nil {
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
