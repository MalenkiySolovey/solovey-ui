//go:build !minimal

package service

import (
	"context"
	"sync"
	"time"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
)

const remoteOutboundAutoRefreshTick = time.Minute

var (
	remoteOutboundAutoMu   sync.Mutex
	remoteOutboundAutoStop context.CancelFunc
	remoteOutboundAutoDone chan struct{}
)

func StartRemoteOutboundAutoRefresh(runtime *coreservice.Runtime) {
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
				if _, err := service.RefreshDueSubscriptionsContext(ctx, "system"); err != nil && ctx.Err() == nil {
					logger.Warning("remote subscription auto refresh scan failed: ", err)
				}
			}
		}
	}()
}

func StopRemoteOutboundAutoRefresh(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	remoteOutboundAutoMu.Lock()
	stop := remoteOutboundAutoStop
	done := remoteOutboundAutoDone
	remoteOutboundAutoMu.Unlock()

	if stop == nil {
		return nil
	}
	stop()
	select {
	case <-done:
		remoteOutboundAutoMu.Lock()
		if remoteOutboundAutoDone == done {
			remoteOutboundAutoStop = nil
			remoteOutboundAutoDone = nil
		}
		remoteOutboundAutoMu.Unlock()
		return nil
	case <-ctx.Done():
		select {
		case <-done:
			remoteOutboundAutoMu.Lock()
			if remoteOutboundAutoDone == done {
				remoteOutboundAutoStop = nil
				remoteOutboundAutoDone = nil
			}
			remoteOutboundAutoMu.Unlock()
			return nil
		default:
			return ctx.Err()
		}
	}
}
