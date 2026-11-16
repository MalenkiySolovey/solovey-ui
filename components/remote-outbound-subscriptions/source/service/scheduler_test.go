//go:build !minimal

package remotesubservice

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSchedulerRequiresAdmissionAndDrainsCancellation(t *testing.T) {
	var scheduler Scheduler
	if scheduler.stop != nil || scheduler.done != nil {
		t.Fatal("construction started work")
	}
	ticks := make(chan time.Time)
	entered := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	closed := make(chan struct{})
	scheduler.mu.Lock()
	scheduler.startLocked(context.Background(), ticks, func() { close(closed) }, func(ctx context.Context) { close(entered); <-ctx.Done(); close(canceled); <-release })
	scheduler.mu.Unlock()
	select {
	case <-entered:
		t.Fatal("work ran before the owned tick")
	default:
	}
	if scheduler.Start(context.Background(), func(context.Context) { t.Error("duplicate worker") }) {
		t.Fatal("duplicate scheduler admitted")
	}
	ticks <- time.Now()
	<-entered
	stopCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := scheduler.Stop(stopCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("undrained work reported stopped: %v", err)
	}
	<-canceled
	if scheduler.Start(context.Background(), func(context.Context) {}) {
		t.Fatal("restarted before draining previous work")
	}
	close(release)
	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-closed
	if scheduler.stop != nil || scheduler.done != nil {
		t.Fatal("drained worker retained active state")
	}
	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerParentCancellationHasNoRefresh(t *testing.T) {
	var scheduler Scheduler
	ticks := make(chan time.Time)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scheduler.mu.Lock()
	scheduler.startLocked(ctx, ticks, func() { close(done) }, func(context.Context) { t.Error("canceled component fetched a source") })
	scheduler.mu.Unlock()
	<-done
	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerStopRetainsTimedOutLifecycleForRetry(t *testing.T) {
	var scheduler Scheduler
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	done := make(chan struct{})
	scheduler.mu.Lock()
	scheduler.stop = cancelWorker
	scheduler.done = done
	scheduler.mu.Unlock()
	t.Cleanup(func() {
		cancelWorker()
	})

	timedOut, cancel := context.WithCancel(context.Background())
	cancel()
	if err := scheduler.Stop(timedOut); !errors.Is(err, context.Canceled) {
		t.Fatalf("scheduler.Stop error = %v, want context cancellation", err)
	}
	if workerCtx.Err() == nil {
		t.Fatal("timed-out Stop did not cancel the worker")
	}
	scheduler.mu.Lock()
	retained := scheduler.done == done && scheduler.stop != nil
	scheduler.mu.Unlock()
	if !retained {
		t.Fatal("timed-out Stop discarded lifecycle state required for retry")
	}

	close(done)
	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	scheduler.mu.Lock()
	cleared := scheduler.done == nil && scheduler.stop == nil
	scheduler.mu.Unlock()
	if !cleared {
		t.Fatal("successful retry retained stopped worker state")
	}
}
