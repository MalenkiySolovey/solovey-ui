//go:build !minimal

package paidsubscriptions

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	paidsettings "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestPaidDropDataRemovesOwnedTablesAndSettings(t *testing.T) {
	initPaidComponentTestDB(t)
	c := &component{}
	if err := c.Migrate(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&model.Setting{Key: paidsettings.EnabledKey, Value: "true"}).Error; err != nil {
		t.Fatal(err)
	}

	if err := c.DropData(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"paidsub_bindings", "tariffs", "payment_orders"} {
		if dbsqlite.DB().Migrator().HasTable(table) {
			t.Fatalf("paid DropData left table %s", table)
		}
	}
	var settingsCount int64
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key IN ?", paidsettings.AllKeys()).Count(&settingsCount).Error; err != nil {
		t.Fatal(err)
	}
	if settingsCount != 0 {
		t.Fatalf("paid DropData left %d setting rows", settingsCount)
	}
}

func initPaidComponentTestDB(t *testing.T) {
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
