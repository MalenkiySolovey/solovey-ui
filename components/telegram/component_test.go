//go:build !minimal

package telegram

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
	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/robfig/cron/v3"
)

func TestManifestDurableSettingsMatchRuntimeOwnership(t *testing.T) {
	secrets := telegramsettings.EncryptedKeys()
	wantSecrets := make([]string, 0, len(secrets))
	for key := range secrets {
		wantSecrets = append(wantSecrets, key)
	}
	wantSettings := make([]string, 0, len(telegramsettings.Defaults())-len(secrets))
	for key := range telegramsettings.Defaults() {
		if _, secret := secrets[key]; !secret {
			wantSettings = append(wantSettings, key)
		}
	}
	sort.Strings(wantSecrets)
	sort.Strings(wantSettings)
	if !slices.Equal(telegramManifest.Database.Secrets, wantSecrets) {
		t.Fatalf("manifest secrets = %v, runtime ownership = %v", telegramManifest.Database.Secrets, wantSecrets)
	}
	if !slices.Equal(telegramManifest.Database.Settings, wantSettings) {
		t.Fatalf("manifest settings = %v, runtime ownership = %v", telegramManifest.Database.Settings, wantSettings)
	}
}

type telegramTrackingScheduler struct {
	added   int
	removed int
}

func (s *telegramTrackingScheduler) AddJob(string, cron.Job) (cron.EntryID, error) {
	s.added++
	return cron.EntryID(s.added), nil
}
func (*telegramTrackingScheduler) Schedule(cron.Schedule, cron.Job) cron.EntryID { return 0 }
func (s *telegramTrackingScheduler) RemoveJob(cron.EntryID)                      { s.removed++ }
func (s *telegramTrackingScheduler) RemoveJobAndWait(context.Context, cron.EntryID) error {
	s.removed++
	return nil
}

func TestTelegramComponentStartIsIdempotent(t *testing.T) {
	scheduler := &telegramTrackingScheduler{}
	c := &component{}
	host := lifecycle.Context{Host: componenthost.Deps{
		Scheduler: scheduler,
		API:       componenthost.APIDeps{Runtime: service.NewRuntimeWithCoreProvider(nil)},
	}}
	if err := c.Start(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	if scheduler.added != 3 {
		t.Fatalf("repeated Start added %d jobs", scheduler.added)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scheduler.removed != 3 {
		t.Fatalf("repeated Stop removed %d jobs", scheduler.removed)
	}
}

func TestTelegramComponentStartRequiresSchedulerWithoutRetainedState(t *testing.T) {
	c := &component{}
	if err := c.start(context.Background(), nil, nil); err == nil {
		t.Fatal("Start accepted a missing scheduler")
	}
	if c.started || c.notifier != nil || c.scheduler != nil || len(c.entryIDs) != 0 ||
		c.unregisterEvent != nil || c.unregisterContribution != nil || c.unregisterBackupCodecs != nil ||
		c.unregisterSettings != nil || c.unregisterLogCategory != nil || c.unregisterTokenScope != nil {
		t.Fatalf("failed Start retained component state: %#v", c)
	}
}

func TestTelegramComponentStartRequiresRuntimeWithoutRetainedState(t *testing.T) {
	c := &component{}
	if err := c.start(context.Background(), &telegramTrackingScheduler{}, nil); err == nil {
		t.Fatal("Start accepted a missing runtime")
	}
	if c.started || c.notifier != nil || c.scheduler != nil || len(c.entryIDs) != 0 ||
		c.unregisterEvent != nil || c.unregisterContribution != nil || c.unregisterBackupCodecs != nil ||
		c.unregisterSettings != nil || c.unregisterLogCategory != nil || c.unregisterTokenScope != nil {
		t.Fatalf("failed Start retained component state: %#v", c)
	}
}

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
