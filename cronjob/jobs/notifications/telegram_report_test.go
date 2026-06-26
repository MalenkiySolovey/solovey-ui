package notifications

import (
	"github.com/MalenkiySolovey/solovey-ui/cronjob/internal/testutil"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/robfig/cron/v3"
)

func TestTelegramReportSchedulerReplansFromSettings(t *testing.T) {
	testutil.InitDatabase(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"telegramReport":     "true",
		"telegramReportCron": "*/5 * * * *",
	} {
		if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", key).Update("value", value).Error; err != nil {
			t.Fatal(err)
		}
	}
	c := cron.New()
	scheduler := NewTelegramReportScheduler(c)
	scheduler.Run()
	if scheduler.entryID == 0 || scheduler.currentSpec != "*/5 * * * *" {
		t.Fatalf("scheduler did not add report job: %#v", scheduler)
	}

	if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", "telegramReport").Update("value", "false").Error; err != nil {
		t.Fatal(err)
	}
	scheduler.Run()
	if scheduler.entryID != 0 || scheduler.currentSpec != "" {
		t.Fatalf("scheduler did not remove report job: %#v", scheduler)
	}
}
