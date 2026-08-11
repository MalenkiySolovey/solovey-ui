package hostsurface

import (
	"context"
	"math/rand"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
)

// RunReconciler performs an immediate read-only reconciliation and then uses a
// jittered 60-second cadence. The trigger is bounded to one pending event.
func RunReconciler(ctx context.Context, trigger <-chan struct{}) {
	for {
		_ = hostfacts.Reconcile(ctx)
		jitter := time.Duration(rand.Int63n(int64(12*time.Second))) - 6*time.Second
		timer := time.NewTimer(60*time.Second + jitter)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-trigger:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
	}
}
