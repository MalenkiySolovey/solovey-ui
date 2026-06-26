package maintenance

import (
	"time"

	ipmonitor "github.com/MalenkiySolovey/solovey-ui/ipmonitor"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type HistoryRetentionJob struct {
	settings *service.SettingService
	audit    *service.AuditService
	pruneIPs func(int64) (int64, error)
	now      func() time.Time
}

func NewHistoryRetentionJob() *HistoryRetentionJob {
	return &HistoryRetentionJob{
		settings: &service.SettingService{},
		audit:    &service.AuditService{},
		pruneIPs: ipmonitor.PruneOlderThan,
		now:      time.Now,
	}
}

func (s *HistoryRetentionJob) Run() {
	auditRetentionDays, err := s.settings.GetAuditRetentionDays()
	if err != nil {
		logger.Warning("Reading audit retention failed: ", err)
	} else if auditRetentionDays > 0 {
		before := s.now().Add(-time.Duration(auditRetentionDays) * 24 * time.Hour).Unix()
		if _, err := s.audit.PruneOlderThan(before); err != nil {
			logger.Warning("Deleting old audit events failed: ", err)
		}
	}

	ipRetentionDays, err := s.settings.GetIPHistoryRetentionDays()
	if err != nil {
		logger.Warning("Reading IP history retention failed: ", err)
	} else if ipRetentionDays > 0 {
		before := s.now().Add(-time.Duration(ipRetentionDays) * 24 * time.Hour).Unix()
		if _, err := s.pruneIPs(before); err != nil {
			logger.Warning("Deleting old client IP history failed: ", err)
		}
	}
}
