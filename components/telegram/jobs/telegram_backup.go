//go:build !minimal

package jobs

import (
	"context"
	"strings"
	"sync"

	telegramschedule "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/schedule"
	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/robfig/cron/v3"
)

type TelegramBackupJob struct {
	telegramservice.TelegramBackupService
}

func NewTelegramBackupJob(runtime telegramservice.RuntimeProvider) *TelegramBackupJob {
	settings := telegramsettings.Reader{}
	return &TelegramBackupJob{
		TelegramBackupService: telegramservice.TelegramBackupService{
			Settings: settings,
			Telegram: &telegramservice.Service{Settings: settings, Runtime: runtime},
			Audit:    recordTelegramBackupAudit,
		},
	}
}

func (j *TelegramBackupJob) Run() {
	j.TelegramBackupService.RunOnce(context.Background(), telegramservice.TelegramBackupTriggerScheduled)
}

func recordTelegramBackupAudit(record telegramservice.AuditRecord) error {
	return (&service.AuditService{}).Record(service.AuditEvent{
		Actor:    record.Actor,
		Event:    record.Event,
		Resource: record.Resource,
		Severity: record.Severity,
		Details:  record.Details,
	})
}

type TelegramBackupScheduler struct {
	Settings telegramsettings.Reader
	Runtime  telegramservice.RuntimeProvider

	cron        entryScheduler
	mu          sync.Mutex
	currentSpec string
	entryID     cron.EntryID
}

func NewTelegramBackupScheduler(c entryScheduler, runtime telegramservice.RuntimeProvider) *TelegramBackupScheduler {
	return &TelegramBackupScheduler{cron: c, Settings: telegramsettings.Reader{}, Runtime: runtime}
}

func (s *TelegramBackupScheduler) Run() {
	telegramEnabled, err := s.Settings.GetTelegramEnabled()
	if err != nil {
		logger.Warning("telegram backup telegram-enabled setting read failed:", err)
		return
	}
	backupEnabled, err := s.Settings.GetTelegramBackupEnabled()
	if err != nil {
		logger.Warning("telegram backup enabled setting read failed:", err)
		return
	}
	spec, err := s.Settings.GetTelegramBackupCron()
	if err != nil {
		logger.Warning("telegram backup cron read failed:", err)
		return
	}
	spec = strings.TrimSpace(spec)
	if !telegramEnabled || !backupEnabled {
		spec = ""
	}
	if err := s.reconcile(spec); err != nil {
		logger.Warning("telegram backup scheduler failed:", err)
	}
}

func (s *TelegramBackupScheduler) Stop() {
	if err := s.reconcile(""); err != nil {
		logger.Warning("telegram backup scheduler stop failed:", err)
	}
}

func (s *TelegramBackupScheduler) reconcile(spec string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if spec == s.currentSpec {
		return nil
	}
	if s.entryID != 0 {
		s.cron.RemoveJob(s.entryID)
		s.entryID = 0
	}
	s.currentSpec = ""
	if spec == "" {
		return nil
	}
	schedule, err := telegramschedule.Parse(spec)
	if err != nil {
		return err
	}
	entryID := s.cron.Schedule(schedule, NewTelegramBackupJob(s.Runtime))
	s.entryID = entryID
	s.currentSpec = spec
	return nil
}
