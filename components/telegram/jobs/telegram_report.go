//go:build !minimal

package jobs

import (
	"context"
	"errors"
	"sync"
	"time"

	telegramschedule "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/schedule"
	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/robfig/cron/v3"
)

type TelegramReportJob struct{}

func NewTelegramReportJob() *TelegramReportJob {
	return &TelegramReportJob{}
}

func (j *TelegramReportJob) Run() {
	j.RunContext(context.Background())
}

func (j *TelegramReportJob) RunContext(ctx context.Context) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	service.NotifyPanelEvent("scheduled_report", map[string]string{
		"ts": time.Now().UTC().Format(time.RFC3339),
	})
}

type TelegramReportScheduler struct {
	Settings telegramsettings.Reader

	cron        entryScheduler
	mu          sync.Mutex
	currentSpec string
	entryID     cron.EntryID
}

type entryScheduler interface {
	Schedule(cron.Schedule, cron.Job) cron.EntryID
	RemoveJobAndWait(context.Context, cron.EntryID) error
}

func NewTelegramReportScheduler(c entryScheduler) *TelegramReportScheduler {
	return &TelegramReportScheduler{cron: c, Settings: telegramsettings.Reader{}}
}

func (s *TelegramReportScheduler) Run() {
	s.RunContext(context.Background())
}

func (s *TelegramReportScheduler) RunContext(ctx context.Context) {
	telegramEnabled, err := s.Settings.GetTelegramEnabled()
	if err != nil {
		s.stopAfterSettingsError(ctx, "telegram enabled", err)
		return
	}
	enabled, err := s.Settings.GetTelegramReport()
	if err != nil {
		s.stopAfterSettingsError(ctx, "report enabled", err)
		return
	}
	spec, err := s.Settings.GetTelegramReportCron()
	if err != nil {
		s.stopAfterSettingsError(ctx, "report cron", err)
		return
	}
	if !telegramEnabled || !enabled {
		spec = ""
	}
	if err := s.reconcile(ctx, spec); err != nil {
		logger.Warning("telegram report scheduler failed:", err)
	}
}

func (s *TelegramReportScheduler) stopAfterSettingsError(ctx context.Context, setting string, err error) {
	logger.Warning("telegram ", setting, " setting read failed:", err)
	if stopErr := s.reconcile(ctx, ""); stopErr != nil {
		logger.Warning("telegram report scheduler fail-closed stop failed:", stopErr)
	}
}

func (s *TelegramReportScheduler) Stop() {
	if err := s.StopContext(context.Background()); err != nil {
		logger.Warning("telegram report scheduler stop failed:", err)
	}
}

func (s *TelegramReportScheduler) StopContext(ctx context.Context) error {
	return s.reconcile(ctx, "")
}

func (s *TelegramReportScheduler) reconcile(ctx context.Context, spec string) error {
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
	entryID := s.cron.Schedule(schedule, NewTelegramReportJob())
	if entryID == 0 {
		return errors.New("telegram report scheduler is unavailable")
	}
	s.entryID = entryID
	s.currentSpec = spec
	return nil
}
