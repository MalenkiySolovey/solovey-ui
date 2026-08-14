//go:build !minimal

package observabilityextra

import (
	"context"
	"errors"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/robfig/cron/v3"
)

type observabilityTrackingScheduler struct {
	added     int
	removed   int
	removeErr error
}

func (s *observabilityTrackingScheduler) AddJob(string, cron.Job) (cron.EntryID, error) {
	s.added++
	return cron.EntryID(s.added), nil
}
func (*observabilityTrackingScheduler) Schedule(cron.Schedule, cron.Job) cron.EntryID { return 0 }
func (s *observabilityTrackingScheduler) RemoveJob(cron.EntryID)                      { s.removed++ }
func (s *observabilityTrackingScheduler) RemoveJobAndWait(context.Context, cron.EntryID) error {
	s.removed++
	return s.removeErr
}

func TestComponentStopRetainsLifecycleStateWhenJobDoesNotStop(t *testing.T) {
	scheduler := &observabilityTrackingScheduler{removeErr: errors.New("job still running")}
	c := &component{}
	host := lifecycle.Context{Host: componenthost.Deps{Scheduler: scheduler}}
	if err := c.Start(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(context.Background()); err == nil {
		t.Fatal("Stop unexpectedly ignored a running job")
	}
	if !c.started || c.scheduler != scheduler || c.entryID == 0 {
		t.Fatalf("failed Stop discarded retry state: %#v", c)
	}
	scheduler.removeErr = nil
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.started || c.scheduler != nil || c.entryID != 0 {
		t.Fatalf("successful Stop retained runtime state: %#v", c)
	}
}

func TestComponentStartIsIdempotent(t *testing.T) {
	scheduler := &observabilityTrackingScheduler{}
	c := &component{}
	host := lifecycle.Context{Host: componenthost.Deps{Scheduler: scheduler}}
	if err := c.Start(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	if scheduler.added != 1 {
		t.Fatalf("repeated Start added %d jobs", scheduler.added)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scheduler.removed != 1 {
		t.Fatalf("repeated Stop removed %d jobs", scheduler.removed)
	}
}

func TestComponentStartFailsWhenSchedulerCapabilityIsUnavailable(t *testing.T) {
	c := &component{}
	if err := c.Start(context.Background(), lifecycle.Context{}); err == nil {
		t.Fatal("Start must fail closed without a scheduler capability")
	}
	if c.started {
		t.Fatal("failed Start must not leave runtime residue")
	}
}
