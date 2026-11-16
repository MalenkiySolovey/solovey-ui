//go:build !minimal

package service

import (
	"context"
	feature "github.com/MalenkiySolovey/solovey-component-remote-outbound-subscriptions/service"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
)

var sourceScheduler feature.Scheduler

func StartRemoteOutboundAutoRefresh(runtime *coreservice.Runtime, outboundHTTP componenthost.OutboundHTTPClientProvider) {
	service := &RemoteOutboundService{Runtime: runtime, OutboundHTTP: outboundHTTP}
	healthSchedule := &SourceHealthSchedule{}
	sourceScheduler.Start(context.Background(), func(ctx context.Context) {
		if _, err := service.RefreshDueSubscriptionsContext(ctx, "system"); err != nil && ctx.Err() == nil {
			logger.Warning("remote subscription auto refresh scan failed: ", err)
		}
		if _, err := service.ProbeDueSubscriptionsContext(ctx, healthSchedule); err != nil && ctx.Err() == nil {
			logger.Warning("remote subscription health scan failed: ", err)
		}
	})
}
func StopRemoteOutboundAutoRefresh(ctx context.Context) error { return sourceScheduler.Stop(ctx) }
