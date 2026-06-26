package maintenance

import (
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type StatisticsRetentionJob struct {
	service.StatsService
	trafficAge int
}

func NewStatisticsRetentionJob(ta int) *StatisticsRetentionJob {
	return &StatisticsRetentionJob{
		trafficAge: ta,
	}
}

func (s *StatisticsRetentionJob) Run() {
	err := s.StatsService.DelOldStats(s.trafficAge)
	if err != nil {
		logger.Warning("Deleting old statistics failed: ", err)
		return
	}
	logger.Debug("Stats older than ", s.trafficAge, " days were deleted")
}
