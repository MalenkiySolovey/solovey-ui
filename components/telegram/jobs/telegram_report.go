//go:build !minimal

package jobs

import (
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
	RemoveJob(cron.EntryID)
}

func NewTelegramReportScheduler(c entryScheduler) *TelegramReportScheduler {
	return &TelegramReportScheduler{cron: c, Settings: telegramsettings.Reader{}}
}

func (s *TelegramReportScheduler) Run() {
	enabled, err := s.Settings.GetTelegramReport()
	if err != nil {
		logger.Warning("telegram report setting read failed:", err)
		return
	}
	spec, err := s.Settings.GetTelegramReportCron()
	if err != nil {
		logger.Warning("telegram report cron read failed:", err)
		return
	}
	if !enabled {
		spec = ""
	}
	if err := s.reconcile(spec); err != nil {
		logger.Warning("telegram report scheduler failed:", err)
	}
}

func (s *TelegramReportScheduler) Stop() {
	if err := s.reconcile(""); err != nil {
		logger.Warning("telegram report scheduler stop failed:", err)
	}
}

func (s *TelegramReportScheduler) reconcile(spec string) error {
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
	entryID := s.cron.Schedule(schedule, NewTelegramReportJob())
	s.entryID = entryID
	s.currentSpec = spec
	return nil
}
