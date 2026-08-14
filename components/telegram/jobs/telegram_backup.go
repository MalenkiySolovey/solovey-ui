//go:build !minimal

package jobs

import (
	"context"
	"errors"
	"strings"
	"sync"

	telegramschedule "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/schedule"
	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/robfig/cron/v3"
)

type TelegramBackupJob struct {
	telegramservice.TelegramBackupService
}

func NewTelegramBackupJob(runtime telegramservice.RuntimeProvider, audit ...func(telegramservice.AuditRecord) error) *TelegramBackupJob {
	settings := telegramsettings.Reader{}
	var auditRecord func(telegramservice.AuditRecord) error
	if len(audit) > 0 {
		auditRecord = audit[0]
	}
	return &TelegramBackupJob{
		TelegramBackupService: telegramservice.TelegramBackupService{
			Settings: settings,
			Telegram: &telegramservice.Service{Settings: settings, Runtime: runtime},
			Audit:    auditRecord,
		},
	}
}

func (j *TelegramBackupJob) Run() {
	j.RunContext(context.Background())
}

func (j *TelegramBackupJob) RunContext(ctx context.Context) {
	j.TelegramBackupService.RunOnce(ctx, telegramservice.TelegramBackupTriggerScheduled)
}

type TelegramBackupScheduler struct {
	Settings telegramsettings.Reader
	Runtime  telegramservice.RuntimeProvider
	Audit    func(telegramservice.AuditRecord) error

	cron        entryScheduler
	mu          sync.Mutex
	currentSpec string
	entryID     cron.EntryID
}

func NewTelegramBackupScheduler(c entryScheduler, runtime telegramservice.RuntimeProvider, audit ...func(telegramservice.AuditRecord) error) *TelegramBackupScheduler {
	var auditRecord func(telegramservice.AuditRecord) error
	if len(audit) > 0 {
		auditRecord = audit[0]
	}
	return &TelegramBackupScheduler{cron: c, Settings: telegramsettings.Reader{}, Runtime: runtime, Audit: auditRecord}
}

func (s *TelegramBackupScheduler) Run() {
	s.RunContext(context.Background())
}

func (s *TelegramBackupScheduler) RunContext(ctx context.Context) {
	telegramEnabled, err := s.Settings.GetTelegramEnabled()
	if err != nil {
		s.stopAfterSettingsError(ctx, "telegram enabled", err)
		return
	}
	backupEnabled, err := s.Settings.GetTelegramBackupEnabled()
	if err != nil {
		s.stopAfterSettingsError(ctx, "backup enabled", err)
		return
	}
	spec, err := s.Settings.GetTelegramBackupCron()
	if err != nil {
		s.stopAfterSettingsError(ctx, "backup cron", err)
		return
	}
	spec = strings.TrimSpace(spec)
	if !telegramEnabled || !backupEnabled {
		spec = ""
	}
	if err := s.reconcile(ctx, spec); err != nil {
		logger.Warning("telegram backup scheduler failed:", err)
	}
}

func (s *TelegramBackupScheduler) stopAfterSettingsError(ctx context.Context, setting string, err error) {
	logger.Warning("telegram ", setting, " setting read failed:", err)
	if stopErr := s.reconcile(ctx, ""); stopErr != nil {
		logger.Warning("telegram backup scheduler fail-closed stop failed:", stopErr)
	}
}

func (s *TelegramBackupScheduler) Stop() {
	if err := s.StopContext(context.Background()); err != nil {
		logger.Warning("telegram backup scheduler stop failed:", err)
	}
}

func (s *TelegramBackupScheduler) StopContext(ctx context.Context) error {
	return s.reconcile(ctx, "")
}

func (s *TelegramBackupScheduler) reconcile(ctx context.Context, spec string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if spec == s.currentSpec {
		return nil
	}
	if s.entryID != 0 {
		if err := s.cron.RemoveJobAndWait(ctx, s.entryID); err != nil {
			return err
		}
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
	entryID := s.cron.Schedule(schedule, NewTelegramBackupJob(s.Runtime, s.Audit))
	if entryID == 0 {
		return errors.New("telegram backup scheduler is unavailable")
	}
	s.entryID = entryID
	s.currentSpec = spec
	return nil
}
