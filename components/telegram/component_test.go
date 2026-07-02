//go:build !minimal

package telegram

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestTelegramDropDataRemovesOwnedSettings(t *testing.T) {
	initTelegramComponentTestDB(t)
	if err := dbsqlite.DB().Create(&model.Setting{Key: telegramsettings.BotTokenKey, Value: "secret"}).Error; err != nil {
		t.Fatal(err)
	}

	if err := (&component{}).DropData(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	var settingsCount int64
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key IN ?", telegramsettings.AllKeys()).Count(&settingsCount).Error; err != nil {
		t.Fatal(err)
	}
	if settingsCount != 0 {
		t.Fatalf("telegram DropData left %d setting rows", settingsCount)
	}
}

func initTelegramComponentTestDB(t *testing.T) {
	t.Helper()
	tempDir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", tempDir)
	if err := dbsqlite.Init(filepath.Join(tempDir, "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	testDB := dbsqlite.DB()
	t.Cleanup(func() {
		if testDB != nil {
			if sqlDB, err := testDB.DB(); err == nil {
				_ = sqlDB.Close()
				time.Sleep(25 * time.Millisecond)
			}
		}
	})
}
