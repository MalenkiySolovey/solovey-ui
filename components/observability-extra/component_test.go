//go:build !minimal

package observabilityextra

import (
	"context"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/robfig/cron/v3"
)

type observabilityTrackingScheduler struct {
	added   int
	removed int
}

func (s *observabilityTrackingScheduler) AddJob(string, cron.Job) (cron.EntryID, error) {
	s.added++
	return cron.EntryID(s.added), nil
}
func (*observabilityTrackingScheduler) Schedule(cron.Schedule, cron.Job) cron.EntryID { return 0 }
func (s *observabilityTrackingScheduler) RemoveJob(cron.EntryID)                      { s.removed++ }

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
