//go:build !minimal

package observabilityextra

import (
	"context"
	"errors"
	hostports "github.com/MalenkiySolovey/solovey-host/componenthost"
	observabilitysvc "github.com/MalenkiySolovey/solovey-ui/service/observability"
	"github.com/robfig/cron/v3"
	"testing"
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

type trackingSampler struct {
	observations, cores int
}

func TestDatabaseBindingFollowsConfirmedLifecycleStop(t *testing.T) {
	scheduler := &observabilityTrackingScheduler{removeErr: errors.New("job still running")}
	c := &component{}
	ports := hostports.Ports{Scheduler: scheduler, ObservationSampler: &trackingSampler{}}
	if err := c.Start(context.Background(), ports); err != nil {
		t.Fatal(err)
	}
	if c.unbindReset == nil {
		t.Fatal("active component did not bind database reset")
	}
	if err := c.Stop(context.Background()); err == nil || c.unbindReset == nil {
		t.Fatal("failed stop discarded Backend binding")
	}
	scheduler.removeErr = nil
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.unbindReset != nil {
		t.Fatal("successful stop retained Backend binding")
	}
}

func (*trackingSampler) CurrentObservabilitySample() observabilitysvc.ObservabilitySample {
	return observabilitysvc.ObservabilitySample{}
}
func (*trackingSampler) CurrentCoreSample() observabilitysvc.CoreSample {
	return observabilitysvc.CoreSample{}
}
func (s *trackingSampler) RecordObservabilitySample(observabilitysvc.ObservabilityBucket, observabilitysvc.ObservabilitySample) error {
	s.observations++
	return nil
}
func (s *trackingSampler) RecordCoreSample(observabilitysvc.ObservabilityBucket, observabilitysvc.CoreSample) error {
	s.cores++
	return nil
}
func (*trackingSampler) HistoryForBucket(observabilitysvc.ObservabilityBucket) ([]observabilitysvc.ObservabilitySample, error) {
	return nil, nil
}
func (*trackingSampler) CoreHistoryForBucket(observabilitysvc.ObservabilityBucket) ([]observabilitysvc.CoreSample, error) {
	return nil, nil
}
