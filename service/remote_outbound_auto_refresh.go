package service

import (
	"context"
	"sync"
	"time"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

const remoteOutboundAutoRefreshTick = time.Minute

var (
	remoteOutboundAutoMu   sync.Mutex
	remoteOutboundAutoStop context.CancelFunc
	remoteOutboundAutoDone chan struct{}
)

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
