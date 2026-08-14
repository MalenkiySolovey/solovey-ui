package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/cronjob/jobs/maintenance"
	runtimejobs "github.com/MalenkiySolovey/solovey-ui/cronjob/jobs/runtime"
	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	mu       sync.Mutex
	cron     *cron.Cron
	ctx      context.Context
	cancel   context.CancelFunc
	managed  map[cron.EntryID]*managedJob
	stopDone context.Context
}

type contextJob interface {
	RunContext(context.Context)
}

type managedJob struct {
	job    cron.Job
	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	stopped bool
	active  sync.WaitGroup
}

func New() *Scheduler {
	return &Scheduler{}
}

func (c *Scheduler) Start(loc *time.Location, trafficAge int) error {
	if c == nil {
		return fmt.Errorf("cron scheduler is unavailable")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cron != nil {
		return fmt.Errorf("cron scheduler is already started")
	}
	runCtx, cancel := context.WithCancel(context.Background())
	cronInstance := cron.New(
		cron.WithLocation(loc),
		cron.WithSeconds(),
		// Recover keeps a panicking job (e.g. a nil-deref in a goroutine) from
		// taking down the whole panel process; SkipIfStillRunning prevents a
		// slow job from overlapping itself.
		cron.WithChain(
			cron.Recover(cronLogger{}),
			cron.SkipIfStillRunning(cronLogger{}),
		),
	)
	// Start stats job
	if _, err := cronInstance.AddJob("@every 10s", runtimejobs.NewTrafficStatisticsJob(trafficAge > 0)); err != nil {
		cancel()
		return err
	}
	// Start expiry job
	if _, err := cronInstance.AddJob("@every 1m", runtimejobs.NewClientExpiryJob()); err != nil {
		cancel()
		return err
	}
	// Start deleting old stats
	if trafficAge > 0 {
		if _, err := cronInstance.AddJob("@daily", maintenance.NewStatisticsRetentionJob(trafficAge)); err != nil {
			cancel()
			return err
		}
	}
	// Start core if it is not running
	if _, err := cronInstance.AddJob("@every 5s", runtimejobs.NewCoreHealthJob()); err != nil {
		cancel()
		return err
	}
	if _, err := cronInstance.AddJob("@every 5s", runtimejobs.NewFailoverJob(runCtx)); err != nil {
		cancel()
		return err
	}
	// database WAL checkpoint
	if _, err := cronInstance.AddJob("@every 10m", maintenance.NewWALCheckpointJob()); err != nil {
		cancel()
		return err
	}
	// retention cleanup
	if _, err := cronInstance.AddJob("@every 1h", maintenance.NewHistoryRetentionJob()); err != nil {
		cancel()
		return err
	}
	// IP TLS certificate auto-renewal. The job is a cheap no-op unless managed
	// IP certificates are enabled and the stored certificate is close to expiry.
	if _, err := cronInstance.AddJob("@every 12h", maintenance.NewCertificateRenewalJob(runCtx)); err != nil {
		cancel()
		return err
	}

	cronInstance.Start()
	c.cron = cronInstance
	c.ctx = runCtx
	c.cancel = cancel
	c.managed = make(map[cron.EntryID]*managedJob)
	c.stopDone = nil

	return nil
}

func (c *Scheduler) AddJob(spec string, job cron.Job) (cron.EntryID, error) {
	if c == nil {
		return 0, fmt.Errorf("cron scheduler is not started")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cron == nil || c.stopDone != nil {
		return 0, fmt.Errorf("cron scheduler is not started")
	}
	managed := newManagedJob(c.ctx, job)
	entryID, err := c.cron.AddJob(spec, managed)
	if err != nil {
		managed.cancelAndPrevent()
		return 0, err
	}
	c.managed[entryID] = managed
	return entryID, nil
}

func (c *Scheduler) Schedule(schedule cron.Schedule, job cron.Job) cron.EntryID {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cron == nil || c.stopDone != nil {
		return 0
	}
	managed := newManagedJob(c.ctx, job)
	entryID := c.cron.Schedule(schedule, managed)
	if entryID == 0 {
		managed.cancelAndPrevent()
		return 0
	}
	c.managed[entryID] = managed
	return entryID
}

func (c *Scheduler) RemoveJob(id cron.EntryID) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.cron == nil || id == 0 {
		c.mu.Unlock()
		return
	}
	c.cron.Remove(id)
	managed := c.managed[id]
	c.mu.Unlock()
	if managed == nil {
		return
	}
	managed.cancelAndPrevent()
	go func() {
		_ = managed.wait(context.Background())
		c.mu.Lock()
		if c.managed[id] == managed {
			delete(c.managed, id)
		}
		c.mu.Unlock()
	}()
}

func (c *Scheduler) RemoveJobAndWait(ctx context.Context, id cron.EntryID) error {
	if c == nil || id == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	if c.cron != nil {
		c.cron.Remove(id)
	}
	managed := c.managed[id]
	c.mu.Unlock()
	if managed == nil {
		return nil
	}
	managed.cancelAndPrevent()
	if err := managed.wait(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	if c.managed[id] == managed {
		delete(c.managed, id)
	}
	c.mu.Unlock()
	return nil
}

func (c *Scheduler) Stop(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	if c.cron == nil {
		c.mu.Unlock()
		return nil
	}
	if c.stopDone == nil {
		for _, job := range c.managed {
			job.cancelAndPrevent()
		}
		if c.cancel != nil {
			c.cancel()
		}
		c.stopDone = c.cron.Stop()
	}
	done := c.stopDone
	c.mu.Unlock()
	select {
	case <-done.Done():
		c.mu.Lock()
		if c.stopDone != nil && c.stopDone.Done() == done.Done() {
			c.cron = nil
			c.ctx = nil
			c.cancel = nil
			c.managed = nil
			c.stopDone = nil
		}
		c.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newManagedJob(parent context.Context, job cron.Job) *managedJob {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &managedJob{job: job, ctx: ctx, cancel: cancel}
}

func (j *managedJob) Run() {
	if j == nil || j.job == nil {
		return
	}
	j.mu.Lock()
	if j.stopped || j.ctx.Err() != nil {
		j.mu.Unlock()
		return
	}
	j.active.Add(1)
	j.mu.Unlock()
	defer j.active.Done()
	if job, ok := j.job.(contextJob); ok {
		job.RunContext(j.ctx)
		return
	}
	j.job.Run()
}

func (j *managedJob) cancelAndPrevent() {
	if j == nil {
		return
	}
	j.mu.Lock()
	j.stopped = true
	j.cancel()
	j.mu.Unlock()
}

func (j *managedJob) wait(ctx context.Context) error {
	if j == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		j.active.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		select {
		case <-done:
			return nil
		default:
			return ctx.Err()
		}
	}
}
