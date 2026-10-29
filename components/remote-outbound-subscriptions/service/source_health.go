//go:build !minimal

package service

import (
	"context"
	"net"
	"time"

	remotesubservice "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/service/internal"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	subexternal "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/external"
)

type SourceHealthSchedule = remotesubservice.HealthSchedule

func (s *RemoteOutboundService) ProbeDueSubscriptionsContext(ctx context.Context, schedule *SourceHealthSchedule) (int, error) {
	if s.runtime() == nil || s.runtime().Core() == nil || !s.runtime().Core().IsRunning() {
		return 0, nil
	}
	return s.implementation().ProbeDueSubscriptionsContext(ctx, schedule)
}

func (s *RemoteOutboundService) probeSourceOutbound(ctx context.Context, tag, target string, timeout time.Duration) coreruntime.CheckOutboundResult {
	if s.runtime() == nil || s.runtime().Core() == nil {
		return coreruntime.CheckOutboundResult{Error: coreruntime.CheckOutboundErrorCoreUnavailable}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return s.runtime().Core().CheckOutboundWithProbe(ctx, tag, target, coreruntime.ProbeSourceSubscription,
		func(ctx context.Context, dial func(context.Context, string, string) (net.Conn, error)) (uint16, error) {
			return subexternal.ProbeWithDialContext(ctx, target, dial)
		})
}
