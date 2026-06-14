package service

import (
	"context"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/logger"
)

const remoteOutboundAutoRefreshTick = time.Minute

var (
	remoteOutboundRefreshMu sync.Mutex
	remoteOutboundAutoMu    sync.Mutex
	remoteOutboundAutoStop  context.CancelFunc
	remoteOutboundAutoDone  chan struct{}
)

func (s *RemoteOutboundService) RefreshDueSubscriptions(loginUser string) (int, error) {
	remoteOutboundRefreshMu.Lock()
	defer remoteOutboundRefreshMu.Unlock()

	now := time.Now().Unix()
	var subscriptions []model.RemoteOutboundSubscription
	if err := database.GetDB().
		Where("enabled = ? AND auto_update = ? AND update_interval > 0", true, true).
		Where("last_updated = 0 OR last_updated + update_interval <= ?", now).
		Order(sortOrderClause).
		Find(&subscriptions).Error; err != nil {
		return 0, err
	}

	refreshed := 0
	for _, subscription := range subscriptions {
		if _, err := s.refreshSubscription(subscription.Id, loginUser); err != nil {
			logger.Warning("remote subscription auto refresh failed: ", subscription.Name, ": ", err)
			continue
		}
		refreshed++
	}
	return refreshed, nil
}

func StartRemoteOutboundAutoRefresh(runtime *Runtime) {
	remoteOutboundAutoMu.Lock()
	defer remoteOutboundAutoMu.Unlock()
	if remoteOutboundAutoStop != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	remoteOutboundAutoStop = cancel
	remoteOutboundAutoDone = done

	go func() {
		defer close(done)
		ticker := time.NewTicker(remoteOutboundAutoRefreshTick)
		defer ticker.Stop()
		service := &RemoteOutboundService{Runtime: runtime}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := service.RefreshDueSubscriptions("system"); err != nil {
					logger.Warning("remote subscription auto refresh scan failed: ", err)
				}
			}
		}
	}()
}

func StopRemoteOutboundAutoRefresh(ctx context.Context) error {
	remoteOutboundAutoMu.Lock()
	stop := remoteOutboundAutoStop
	done := remoteOutboundAutoDone
	remoteOutboundAutoStop = nil
	remoteOutboundAutoDone = nil
	remoteOutboundAutoMu.Unlock()

	if stop == nil {
		return nil
	}
	stop()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
