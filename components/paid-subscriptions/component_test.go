//go:build !minimal

package paidsubscriptions

import (
	"context"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	paidsettings "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/robfig/cron/v3"
)

func TestManifestDurableSettingsMatchRuntimeOwnership(t *testing.T) {
	secrets := paidsettings.EncryptedKeys()
	wantSecrets := make([]string, 0, len(secrets))
	for key := range secrets {
		wantSecrets = append(wantSecrets, key)
	}
	wantSettings := make([]string, 0, len(paidsettings.Defaults())-len(secrets))
	for key := range paidsettings.Defaults() {
		if _, secret := secrets[key]; !secret {
			wantSettings = append(wantSettings, key)
		}
	}
	sort.Strings(wantSecrets)
	sort.Strings(wantSettings)
	if !slices.Equal(componentManifest.Database.Secrets, wantSecrets) {
		t.Fatalf("manifest secrets = %v, runtime ownership = %v", componentManifest.Database.Secrets, wantSecrets)
	}
	if !slices.Equal(componentManifest.Database.Settings, wantSettings) {
		t.Fatalf("manifest settings = %v, runtime ownership = %v", componentManifest.Database.Settings, wantSettings)
	}
}

type paidTrackingScheduler struct {
	added   int
	removed int
}

func (s *paidTrackingScheduler) AddJob(string, cron.Job) (cron.EntryID, error) {
	s.added++
	return cron.EntryID(s.added), nil
}
func (*paidTrackingScheduler) Schedule(cron.Schedule, cron.Job) cron.EntryID { return 0 }
func (s *paidTrackingScheduler) RemoveJob(cron.EntryID)                      { s.removed++ }

func TestPaidComponentStartIsIdempotent(t *testing.T) {
	initPaidComponentTestDB(t)
	scheduler := &paidTrackingScheduler{}
	c := &component{}
	host := lifecycle.Context{Host: componenthost.Deps{Scheduler: scheduler}}
	if err := c.Start(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	if scheduler.added != 1 {
		t.Fatalf("repeated Start added %d jobs", scheduler.added)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scheduler.removed != 1 {
		t.Fatalf("repeated Stop removed %d jobs", scheduler.removed)
	}
}

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
