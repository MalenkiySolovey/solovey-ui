//go:build !minimal

package observabilityextra

import (
	"context"
	"errors"
	"testing"

	hostlifecycle "github.com/MalenkiySolovey/solovey-host/componenthost"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/observation"
	sharedscheduler "github.com/MalenkiySolovey/solovey-host/scheduler"
	"github.com/robfig/cron/v3"
)

type samplingLifecycleScheduler struct {
	job      *sharedscheduler.ManagedJob
	added    int
	retained bool
	failStop bool
}

func (s *samplingLifecycleScheduler) AddJob(spec string, job cron.Job) (cron.EntryID, error) {
	if spec != "@every 2s" {
		return 0, errors.New("unexpected sampling schedule")
	}
	s.added++
	s.retained = true
	s.job = sharedscheduler.NewManagedJob(context.Background(), job, func(ctx context.Context) (context.Context, func(), error) { return ctx, func() {}, nil })
	return cron.EntryID(s.added), nil
}
func (*samplingLifecycleScheduler) Schedule(cron.Schedule, cron.Job) cron.EntryID { return 0 }
func (s *samplingLifecycleScheduler) RemoveJob(cron.EntryID) {
	if s.job != nil {
		s.job.CancelAndPrevent()
	}
}
func (s *samplingLifecycleScheduler) RemoveJobAndWait(ctx context.Context, _ cron.EntryID) error {
	if s.failStop {
		return errors.New("sampling drain incomplete")
	}
	s.job.CancelAndPrevent()
	if err := s.job.Wait(ctx); err != nil {
		return err
	}
	s.retained = false
	return nil
}

type samplingLifecycleOwner struct{ observations, cores int }

func (*samplingLifecycleOwner) CurrentObservabilitySample() observation.ObservabilitySample {
	return observation.ObservabilitySample{DateTime: 1}
}
func (*samplingLifecycleOwner) CurrentCoreSample() observation.CoreSample {
	return observation.CoreSample{DateTime: 1}
}
func (s *samplingLifecycleOwner) RecordObservabilitySample(observation.ObservabilityBucket, observation.ObservabilitySample) error {
	s.observations++
	return nil
}
func (s *samplingLifecycleOwner) RecordCoreSample(observation.ObservabilityBucket, observation.CoreSample) error {
	s.cores++
	return nil
}
func (*samplingLifecycleOwner) HistoryForBucket(observation.ObservabilityBucket) ([]observation.ObservabilitySample, error) {
	return nil, nil
}
func (*samplingLifecycleOwner) CoreHistoryForBucket(observation.ObservabilityBucket) ([]observation.CoreSample, error) {
	return nil, nil
}

func TestSharedLoopRunsPanelSamplingLifecycleAndRetainsFailedDrain(t *testing.T) {
	schedule := &samplingLifecycleScheduler{}
	sampler := &samplingLifecycleOwner{}
	owner := &component{}
	entry := hostlifecycle.Entry{ID: componentManifest.ID, Lifecycle: owner,
		Ports: hostlifecycle.Ports{Host: hostref.Local(), Scheduler: schedule, ObservationSampler: sampler}}
	running, err := hostlifecycle.Reconcile(context.Background(), nil, nil, nil)
	if err != nil || schedule.added != 0 {
		t.Fatal("absent component started", err)
	}
	if schedule.added != 0 || owner.unbindReset != nil {
		t.Fatal("construction started component work")
	}
	running, err = hostlifecycle.Reconcile(context.Background(), running, []hostlifecycle.Entry{entry}, nil)
	if err != nil || owner.unbindReset == nil {
		t.Fatal("Panel lifecycle did not bind accepted feature", err)
	}
	running, err = hostlifecycle.Reconcile(context.Background(), running, []hostlifecycle.Entry{entry}, nil)
	if err != nil || schedule.added != 1 {
		t.Fatal("repeated start duplicated sampling", err)
	}
	job := schedule.job
	job.Run()
	if sampler.observations != 1 || sampler.cores != 1 {
		t.Fatal("actual pinned component did not sample")
	}
	schedule.failStop = true
	running, err = hostlifecycle.Reconcile(context.Background(), running, nil, nil)
	if err == nil || len(running) != 1 || !schedule.retained || owner.unbindReset == nil {
		t.Fatal("failed drain lost actual Component or Backend binding")
	}
	schedule.failStop = false
	running, err = hostlifecycle.Stop(context.Background(), running, nil)
	if err != nil || len(running) != 0 || schedule.retained || owner.unbindReset != nil {
		t.Fatal("drain retry did not release Component and Backend binding", err)
	}
	job.Run()
	if sampler.observations != 1 || sampler.cores != 1 {
		t.Fatal("stopped component resumed owned work")
	}
}

func TestSharedLoopRejectsMissingSamplingCapability(t *testing.T) {
	schedule := &samplingLifecycleScheduler{}
	owner := &component{}
	entry := hostlifecycle.Entry{ID: componentManifest.ID, Lifecycle: owner, Ports: hostlifecycle.Ports{Host: hostref.Local(), Scheduler: schedule}}
	running, err := hostlifecycle.Reconcile(context.Background(), nil, []hostlifecycle.Entry{entry}, nil)
	if err == nil || len(running) != 0 || schedule.added != 0 || owner.unbindReset != nil {
		t.Fatal("missing sampling owner became active")
	}
}
