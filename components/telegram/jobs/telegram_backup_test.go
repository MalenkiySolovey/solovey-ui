//go:build !minimal

package jobs

import (
	"context"
	"strings"
	"testing"

	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/robfig/cron/v3"
)

func TestTelegramBackupSchedulerReplansFromSettings(t *testing.T) {
	initDatabase(t)
	registerTelegramSettingsForTest(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	updateTelegramBackupSchedulerSettings(t, map[string]string{
		"telegramEnabled":       "true",
		"telegramBackupEnabled": "true",
		"telegramBackupCron":    "*/5 * * * *",
	})
	c := cron.New()
	scheduler := NewTelegramBackupScheduler(testEntryScheduler{Cron: c}, nil)
	scheduler.Run()
	firstEntry := scheduler.entryID
	if firstEntry == 0 || scheduler.currentSpec != "*/5 * * * *" {
		t.Fatalf("scheduler did not add backup job: %#v", scheduler)
	}

	updateTelegramBackupSchedulerSettings(t, map[string]string{
		"telegramBackupCron": "*/10 * * * *",
	})
	scheduler.Run()
	if scheduler.entryID == 0 || scheduler.entryID == firstEntry || scheduler.currentSpec != "*/10 * * * *" {
		t.Fatalf("scheduler did not replan backup job: %#v", scheduler)
	}
	scheduler.Stop()
	if scheduler.entryID != 0 || scheduler.currentSpec != "" {
		t.Fatalf("scheduler did not stop backup job: %#v", scheduler)
	}
	scheduler.Run()

	updateTelegramBackupSchedulerSettings(t, map[string]string{
		"telegramBackupEnabled": "false",
	})
	scheduler.Run()
	if scheduler.entryID != 0 || scheduler.currentSpec != "" {
		t.Fatalf("scheduler did not remove disabled backup job: %#v", scheduler)
	}
}

func TestTelegramBackupSchedulerNoopWhenTelegramDisabled(t *testing.T) {
	initDatabase(t)
	registerTelegramSettingsForTest(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	updateTelegramBackupSchedulerSettings(t, map[string]string{
		"telegramEnabled":       "false",
		"telegramBackupEnabled": "true",
		"telegramBackupCron":    "*/5 * * * *",
	})
	c := cron.New()
	scheduler := NewTelegramBackupScheduler(testEntryScheduler{Cron: c}, nil)
	scheduler.Run()
	if scheduler.entryID != 0 || scheduler.currentSpec != "" {
		t.Fatalf("scheduler planned while telegram disabled: %#v", scheduler)
	}
}

func TestTelegramBackupSchedulerStopsWhenSettingIsInvalid(t *testing.T) {
	initDatabase(t)
	registerTelegramSettingsForTest(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	updateTelegramBackupSchedulerSettings(t, map[string]string{
		"telegramEnabled":       "true",
		"telegramBackupEnabled": "true",
		"telegramBackupCron":    "*/5 * * * *",
	})
	scheduler := NewTelegramBackupScheduler(testEntryScheduler{Cron: cron.New()}, nil)
	scheduler.Run()
	if scheduler.entryID == 0 {
		t.Fatal("backup scheduler did not establish the initial job")
	}
	updateTelegramBackupSchedulerSettings(t, map[string]string{"telegramBackupEnabled": "invalid"})
	scheduler.Run()
	if scheduler.entryID != 0 || scheduler.currentSpec != "" {
		t.Fatalf("backup scheduler retained a job after a settings failure: %#v", scheduler)
	}
}

func TestTelegramBackupJobUsesScheduledTrigger(t *testing.T) {
	initDatabase(t)
	registerTelegramSettingsForTest(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	runtime := service.NewRuntimeWithCoreProvider(nil)
	job := NewTelegramBackupJob(nil, func(record telegramservice.AuditRecord) error {
		return (&service.AuditService{Runtime: runtime}).Record(service.AuditEvent{
			Actor: record.Actor, Event: record.Event, Resource: record.Resource,
			Severity: record.Severity, Details: record.Details,
		})
	})
	job.Run()
	if err := runtime.StopAuditWriter(context.Background()); err != nil {
		t.Fatal(err)
	}
	var event model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "tg_backup_failed").Order("id desc").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Actor != "system" || !strings.Contains(string(event.Details), `"trigger":"scheduled"`) {
		t.Fatalf("scheduled job did not audit scheduled trigger: %#v details=%s", event, event.Details)
	}
}

func updateTelegramBackupSchedulerSettings(t *testing.T, settings map[string]string) {
	t.Helper()
	for key, value := range settings {
		if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", key).Update("value", value).Error; err != nil {
			t.Fatal(err)
		}
	}
}
