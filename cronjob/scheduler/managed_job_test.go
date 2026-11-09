package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
)

type blockingContextJob struct {
	started sync.Once
	start   chan struct{}
	done    chan struct{}
	block   <-chan struct{}
}

func (j *blockingContextJob) Run() {
	panic("managed scheduler must prefer RunContext")
}

func (j *blockingContextJob) RunContext(ctx context.Context) {
	j.started.Do(func() { close(j.start) })
	if j.block == nil {
		<-ctx.Done()
	} else {
		<-j.block
	}
	close(j.done)
}

func TestRemoveJobAndWaitCancelsContextJob(t *testing.T) {
	scheduler := New()
	if err := scheduler.Start(time.UTC, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scheduler.Stop(context.Background()) })
	job := &blockingContextJob{start: make(chan struct{}), done: make(chan struct{})}
	entryID, err := scheduler.AddJob("@every 10ms", job)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-job.start:
	case <-time.After(3 * time.Second):
		t.Fatal("managed job did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := scheduler.RemoveJobAndWait(ctx, entryID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-job.done:
	default:
		t.Fatal("RemoveJobAndWait returned before the context job stopped")
	}
}

func TestRemoveJobAndWaitRetainsTimedOutHandleForRetry(t *testing.T) {
	scheduler := New()
	if err := scheduler.Start(time.UTC, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scheduler.Stop(context.Background()) })
	release := make(chan struct{})
	job := &blockingContextJob{start: make(chan struct{}), done: make(chan struct{}), block: release}
	entryID, err := scheduler.AddJob("@every 10ms", job)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-job.start:
	case <-time.After(3 * time.Second):
		t.Fatal("managed job did not start")
	}
	shortCtx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := scheduler.RemoveJobAndWait(shortCtx, entryID); err == nil {
		t.Fatal("RemoveJobAndWait unexpectedly ignored its deadline")
	}
	close(release)
	if err := scheduler.RemoveJobAndWait(context.Background(), entryID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-job.done:
	default:
		t.Fatal("retry returned before the retained job finished")
	}
}

func TestStopRetainsTimedOutLifecycleForRetry(t *testing.T) {
	scheduler := New()
	if err := scheduler.Start(time.UTC, 0); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	job := &blockingContextJob{start: make(chan struct{}), done: make(chan struct{}), block: release}
	if _, err := scheduler.AddJob("@every 10ms", job); err != nil {
		t.Fatal(err)
	}
	select {
	case <-job.start:
	case <-time.After(3 * time.Second):
		t.Fatal("managed job did not start")
	}
	shortCtx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := scheduler.Stop(shortCtx); err == nil {
		t.Fatal("Stop unexpectedly ignored its deadline")
	}
	if _, err := scheduler.AddJob("@every 1s", job); err == nil {
		t.Fatal("stopping scheduler accepted a new job")
	}
	close(release)
	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-job.done:
	default:
		t.Fatal("Stop retry returned before the retained job finished")
	}
}

func TestConcurrentStopDrainsScheduledJobAndRejectsNewEntries(t *testing.T) {
	scheduler := New()
	if err := scheduler.Start(time.UTC, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = scheduler.Stop(ctx)
	})
	job := &blockingContextJob{start: make(chan struct{}), done: make(chan struct{})}
	if id := scheduler.Schedule(cron.Every(time.Second), job); id == 0 {
		t.Fatal("scheduled job was not registered")
	}
	select {
	case <-job.start:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduled job did not execute")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	results := make(chan error, 8)
	for index := 0; index < cap(results); index++ {
		go func() { results <- scheduler.Stop(ctx) }()
	}
	for index := 0; index < cap(results); index++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("concurrent Stop exceeded its deadline")
		}
	}
	select {
	case <-job.done:
	default:
		t.Fatal("Stop returned before the shared context job drained")
	}
	if scheduler.Schedule(cron.Every(time.Second), job) != 0 {
		t.Fatal("stopped scheduler accepted a new recurring entry")
	}
}
