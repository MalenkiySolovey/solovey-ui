//go:build !minimal

package jobs

import (
	"context"

	canonical "github.com/MalenkiySolovey/solovey-component-telegram/jobs"
	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/robfig/cron/v3"
)

type CPUAlertJob = canonical.CPUAlertJob
type TelegramReportJob = canonical.TelegramReportJob
type TelegramReportScheduler = canonical.TelegramReportScheduler
type TelegramBackupJob = canonical.TelegramBackupJob
type TelegramBackupScheduler = canonical.TelegramBackupScheduler
type entryScheduler interface {
	Schedule(cron.Schedule, cron.Job) cron.EntryID
	RemoveJobAndWait(context.Context, cron.EntryID) error
}

func NewCPUAlertJob() *CPUAlertJob {
	return canonical.NewCPUAlertJob(telegramsettings.Reader{}, (&service.ServerService{}).GetCpuPercent, service.NotifyPanelEvent)
}
func NewTelegramReportJob() *TelegramReportJob {
	return canonical.NewTelegramReportJob(service.NotifyPanelEvent)
}
func NewTelegramReportScheduler(scheduler entryScheduler) *TelegramReportScheduler {
	return canonical.NewTelegramReportScheduler(scheduler, telegramsettings.Reader{}, service.NotifyPanelEvent, logger.Warning)
}
func NewTelegramBackupJob(runtime telegramservice.RuntimeProvider, audit ...func(telegramservice.AuditRecord) error) *TelegramBackupJob {
	settings := telegramsettings.Reader{}
	var record func(telegramservice.AuditRecord) error
	if len(audit) > 0 {
		record = audit[0]
	}
	owner := &telegramservice.TelegramBackupService{Settings: settings, Telegram: &telegramservice.Service{Settings: settings, Runtime: runtime}, Audit: record}
	return canonical.NewTelegramBackupJob(func(ctx context.Context) { owner.RunOnce(ctx, telegramservice.TelegramBackupTriggerScheduled) })
}
func NewTelegramBackupScheduler(scheduler entryScheduler, runtime telegramservice.RuntimeProvider, audit ...func(telegramservice.AuditRecord) error) *TelegramBackupScheduler {
	return canonical.NewTelegramBackupScheduler(scheduler, telegramsettings.Reader{}, NewTelegramBackupJob(runtime, audit...).RunContext, logger.Warning)
}
