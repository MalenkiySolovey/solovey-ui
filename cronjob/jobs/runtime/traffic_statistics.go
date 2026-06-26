package runtime

import (
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type TrafficStatisticsJob struct {
	service.StatsService
	enableTraffic bool
}

func NewTrafficStatisticsJob(saveTraffic bool) *TrafficStatisticsJob {
	return &TrafficStatisticsJob{
		enableTraffic: saveTraffic,
	}
}

func (s *TrafficStatisticsJob) Run() {
	err := s.StatsService.SaveStats(s.enableTraffic)
	if err != nil {
		logger.Warning("Get stats failed: ", err)
		return
	}
}
