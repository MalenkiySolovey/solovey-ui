//go:build !minimal

package jobs

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/robfig/cron/v3"
)

func TestTelegramReportSchedulerReplansFromSettings(t *testing.T) {
	initDatabase(t)
	registerTelegramSettingsForTest(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"telegramEnabled":    "true",
		"telegramReport":     "true",
		"telegramReportCron": "*/5 * * * *",
	} {
		if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", key).Update("value", value).Error; err != nil {
			t.Fatal(err)
		}
	}
	c := cron.New()
	scheduler := NewTelegramReportScheduler(testEntryScheduler{Cron: c})
	scheduler.Run()
	if scheduler.entryID == 0 || scheduler.currentSpec != "*/5 * * * *" {
		t.Fatalf("scheduler did not add report job: %#v", scheduler)
	}

	scheduler.Stop()
	if scheduler.entryID != 0 || scheduler.currentSpec != "" {
		t.Fatalf("scheduler did not stop report job: %#v", scheduler)
	}

	scheduler.Run()
	if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", "telegramReport").Update("value", "false").Error; err != nil {
		t.Fatal(err)
	}
	scheduler.Run()
	if scheduler.entryID != 0 || scheduler.currentSpec != "" {
		t.Fatalf("scheduler did not remove report job: %#v", scheduler)
	}
}

func TestTelegramReportSchedulerStopsWhenTelegramSettingIsInvalid(t *testing.T) {
	initDatabase(t)
	registerTelegramSettingsForTest(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"telegramEnabled":    "true",
		"telegramReport":     "true",
		"telegramReportCron": "*/5 * * * *",
	} {
		if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", key).Update("value", value).Error; err != nil {
			t.Fatal(err)
		}
	}
	scheduler := NewTelegramReportScheduler(testEntryScheduler{Cron: cron.New()})
	scheduler.Run()
	if scheduler.entryID == 0 {
		t.Fatal("report scheduler did not establish the initial job")
	}
	if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", "telegramEnabled").Update("value", "invalid").Error; err != nil {
		t.Fatal(err)
	}
	scheduler.Run()
	if scheduler.entryID != 0 || scheduler.currentSpec != "" {
		t.Fatalf("report scheduler retained a job after a settings failure: %#v", scheduler)
	}
}
