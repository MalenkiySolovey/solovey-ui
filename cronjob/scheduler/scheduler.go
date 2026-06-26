package scheduler

import (
	"time"

	"github.com/MalenkiySolovey/solovey-ui/cronjob/jobs/maintenance"
	"github.com/MalenkiySolovey/solovey-ui/cronjob/jobs/notifications"
	runtimejobs "github.com/MalenkiySolovey/solovey-ui/cronjob/jobs/runtime"
	"github.com/MalenkiySolovey/solovey-ui/cronjob/jobs/subscriptions"
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
	// CPU hysteresis notifications
	if _, err := c.cron.AddJob("@every 12s", notifications.NewCPUAlertJob()); err != nil {
		return err
	}
	// Observability history sampling
	if _, err := c.cron.AddJob("@every 2s", runtimejobs.NewObservabilitySamplingJob()); err != nil {
		return err
	}
	// Telegram scheduled report dynamic replanning
	reportScheduler := notifications.NewTelegramReportScheduler(c.cron)
	reportScheduler.Run()
	if _, err := c.cron.AddJob("@every 1m", reportScheduler); err != nil {
		return err
	}
	// Telegram encrypted database backup dynamic replanning
	backupScheduler := notifications.NewTelegramBackupScheduler(c.cron)
	backupScheduler.Run()
	if _, err := c.cron.AddJob("@every 1m", backupScheduler); err != nil {
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
	// Paid Subscriptions: poll out-of-band payments + expire stale orders
	if _, err := c.cron.AddJob("@every 20s", subscriptions.NewPaymentPollJob()); err != nil {
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

func (c *Scheduler) Stop() {
	if c.cron != nil {
		c.cron.Stop()
	}
}
