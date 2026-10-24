package app

import (
	"context"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/logger"
	serviceupdate "github.com/MalenkiySolovey/solovey-ui/service/update"
)

// App owns this worker's cancellation and drain. Update owns the bounded
// reconciliation tick and its existing operation/checkpoint state machine.
func (a *APP) startUpdateMaintenanceWatchdog() {
	if a.stopUpdateWatchdog != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); serviceupdate.SharedLifecycle().RunMaintenanceWatchdog(ctx) }()
	a.stopUpdateWatchdog = func() {
		cancel()
		deadline, stop := context.WithTimeout(context.Background(), 35*time.Second)
		defer stop()
		select {
		case <-done:
		case <-deadline.Done():
			logger.Warning("update maintenance watchdog did not drain")
		}
	}
}
