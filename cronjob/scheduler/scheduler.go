package scheduler

import (
	"fmt"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/cronjob/jobs/maintenance"
	runtimejobs "github.com/MalenkiySolovey/solovey-ui/cronjob/jobs/runtime"
	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron *cron.Cron
}

func New() *Scheduler {
	return &Scheduler{}
}

func (c *Scheduler) Start(loc *time.Location, trafficAge int) error {
	c.cron = cron.New(
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
	if _, err := c.cron.AddJob("@every 10s", runtimejobs.NewTrafficStatisticsJob(trafficAge > 0)); err != nil {
		return err
	}
	// Start expiry job
	if _, err := c.cron.AddJob("@every 1m", runtimejobs.NewClientExpiryJob()); err != nil {
		return err
	}
	// Start deleting old stats
	if trafficAge > 0 {
		if _, err := c.cron.AddJob("@daily", maintenance.NewStatisticsRetentionJob(trafficAge)); err != nil {
			return err
		}
	}
	// Start core if it is not running
	if _, err := c.cron.AddJob("@every 5s", runtimejobs.NewCoreHealthJob()); err != nil {
		return err
	}
	if _, err := c.cron.AddJob("@every 5s", runtimejobs.NewFailoverJob()); err != nil {
		return err
	}
	// database WAL checkpoint
	if _, err := c.cron.AddJob("@every 10m", maintenance.NewWALCheckpointJob()); err != nil {
		return err
	}
	// retention cleanup
	if _, err := c.cron.AddJob("@every 1h", maintenance.NewHistoryRetentionJob()); err != nil {
		return err
	}
	// IP TLS certificate auto-renewal. The job is a cheap no-op unless managed
	// IP certificates are enabled and the stored certificate is close to expiry.
	if _, err := c.cron.AddJob("@every 12h", maintenance.NewCertificateRenewalJob()); err != nil {
		return err
	}

	c.cron.Start()

	return nil
}

func (c *Scheduler) AddJob(spec string, job cron.Job) (cron.EntryID, error) {
	if c == nil || c.cron == nil {
		return 0, fmt.Errorf("cron scheduler is not started")
	}
	return c.cron.AddJob(spec, job)
}

func (c *Scheduler) Schedule(schedule cron.Schedule, job cron.Job) cron.EntryID {
	if c == nil || c.cron == nil {
		return 0
	}
	return c.cron.Schedule(schedule, job)
}

func (c *Scheduler) RemoveJob(id cron.EntryID) {
	if c == nil || c.cron == nil || id == 0 {
		return
	}
	c.cron.Remove(id)
}

func (c *Scheduler) Stop() {
	if c.cron != nil {
		c.cron.Stop()
	}
}
