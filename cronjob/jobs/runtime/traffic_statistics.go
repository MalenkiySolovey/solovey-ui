package runtime

import (
	"context"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type TrafficStatisticsJob struct {
	service.StatsService
	enableTraffic bool
	ctx           context.Context
}

func NewTrafficStatisticsJob(saveTraffic bool, contexts ...context.Context) *TrafficStatisticsJob {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	return &TrafficStatisticsJob{
		enableTraffic: saveTraffic,
		ctx:           ctx,
	}
}

func (s *TrafficStatisticsJob) Run() {
	if s.ctx.Err() != nil {
		return
	}
	err := s.StatsService.SaveStats(s.enableTraffic)
	if err != nil {
		logger.Warning("Get stats failed: ", err)
	}
	if err := service.NewConfigServiceWithRuntime(s.StatsService.Runtime).ReconcileWireGuardDevices(s.ctx); err != nil && s.ctx.Err() == nil {
		logger.Warning("WireGuard device reconciliation failed")
	}
}
