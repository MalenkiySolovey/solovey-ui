//go:build !minimal

package telegram

import (
	"context"
	_ "embed"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	telegramhttp "github.com/MalenkiySolovey/solovey-ui/components/telegram/api"
	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/components/telegram/jobs"
	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/diagnostics"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/robfig/cron/v3"
)

//go:embed component.json
var componentJSON []byte

var telegramManifest = manifest.MustFromJSON(componentJSON)
var id = telegramManifest.ID

func init() {
	registry.Register(registry.Component{
		Manifest:  telegramManifest,
		Lifecycle: &component{},
	})
	registry.RegisterAPIRoutes(id, componenthost.RouteAdapter[telegramhttp.Deps]{
		Build:    telegramDeps,
		Register: telegramhttp.RegisterRoutes,
	}.RegisterRoutes)
}

type component struct {
	mu                     sync.Mutex
	started                bool
	scheduler              componenthost.Scheduler
	unregisterEvent        func()
	unregisterContribution func()
	unregisterBackupCodecs func()
	unregisterSettings     func()
	unregisterLogCategory  func()
	unregisterTokenScope   func()
	notifier               *telegramservice.Notifier
	entryIDs               []cron.EntryID
	reportScheduler        *jobs.TelegramReportScheduler
	backupScheduler        *jobs.TelegramBackupScheduler
}

func (c *component) Start(ctx context.Context, lifecycleCtx lifecycle.Context) error {
	return c.start(ctx, lifecycleCtx.Host.Scheduler, lifecycleCtx.Host.API.Runtime)
}

func (c *component) start(ctx context.Context, scheduler componenthost.Scheduler, runtime *service.Runtime) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return nil
	}
	c.started = true
	if c.notifier == nil {
		c.notifier = newTelegramNotifier(runtime)
	}
	notifier := c.notifier
	if c.unregisterEvent == nil {
		c.unregisterEvent = service.RegisterPanelEventNotifier(id, func(event string, fields map[string]string) {
			(&telegramservice.Service{
				Runtime:  runtimeAdapter{Runtime: runtime},
				Notifier: notifier,
				Settings: telegramsettings.Reader{},
			}).NotifyEvent(event, fields)
		})
	}
	if c.unregisterContribution == nil {
		c.unregisterContribution = registerSettingContribution()
	}
	if c.unregisterBackupCodecs == nil {
		c.unregisterBackupCodecs = registerBackupCodecs()
	}
	if c.unregisterSettings == nil {
		c.unregisterSettings = registerSettingsObserver()
	}
	if c.unregisterLogCategory == nil {
		c.unregisterLogCategory = diagnostics.RegisterLogCategory(diagnostics.LogCategoryContribution{
			Category: "telegram",
			Hint:     "Check Telegram bot token/chat settings, proxy/outbound transport, and Bot API network access.",
			Match: func(_ string, message string) bool {
				return strings.Contains(message, "telegram") || strings.Contains(message, "bot api") || strings.Contains(message, "notifier")
			},
		})
	}
	if c.unregisterTokenScope == nil {
		c.unregisterTokenScope = service.RegisterAPITokenScopeProvider(func() []string {
			return []string{"telegram"}
		})
	}
	c.mu.Unlock()
	if scheduler == nil {
		return nil
	}
	runtimeJobs, err := startTelegramRuntimeJobs(scheduler, runtime)
	if err != nil {
		_ = c.Stop(ctx)
		return err
	}

	c.mu.Lock()
	c.scheduler = runtimeJobs.scheduler
	c.entryIDs = runtimeJobs.entryIDs
	c.reportScheduler = runtimeJobs.reportScheduler
	c.backupScheduler = runtimeJobs.backupScheduler
	c.mu.Unlock()
	return nil
}

func (c *component) Stop(ctx context.Context) error {
	c.mu.Lock()
	c.started = false
	scheduler := c.scheduler
	unregisterEvent := c.unregisterEvent
	unregisterContribution := c.unregisterContribution
	unregisterBackupCodecs := c.unregisterBackupCodecs
	unregisterSettings := c.unregisterSettings
	unregisterLogCategory := c.unregisterLogCategory
	unregisterTokenScope := c.unregisterTokenScope
	notifier := c.notifier
	entryIDs := append([]cron.EntryID(nil), c.entryIDs...)
	reportScheduler := c.reportScheduler
	backupScheduler := c.backupScheduler
	c.scheduler = nil
	c.unregisterEvent = nil
	c.unregisterContribution = nil
	c.unregisterBackupCodecs = nil
	c.unregisterSettings = nil
	c.unregisterLogCategory = nil
	c.unregisterTokenScope = nil
	c.notifier = nil
	c.entryIDs = nil
	c.reportScheduler = nil
	c.backupScheduler = nil
	c.mu.Unlock()

	telegramRuntimeJobs{
		scheduler:       scheduler,
		entryIDs:        entryIDs,
		reportScheduler: reportScheduler,
		backupScheduler: backupScheduler,
	}.stop()
	callUnregisters(
		unregisterEvent,
		unregisterBackupCodecs,
		unregisterSettings,
		unregisterLogCategory,
		unregisterTokenScope,
		unregisterContribution,
	)
	if notifier != nil {
		return notifier.Stop(ctx)
	}
	return nil
}

func (c *component) DropData(context.Context, lifecycle.Context) error {
	if dbsqlite.DB() == nil {
		return nil
	}
	return dbsqlite.DB().Where("key IN ?", telegramsettings.AllKeys()).Delete(&model.Setting{}).Error
}

type telegramRuntimeJobs struct {
	scheduler       componenthost.Scheduler
	entryIDs        []cron.EntryID
	reportScheduler *jobs.TelegramReportScheduler
	backupScheduler *jobs.TelegramBackupScheduler
}

func startTelegramRuntimeJobs(scheduler componenthost.Scheduler, runtime *service.Runtime) (telegramRuntimeJobs, error) {
	runtimeJobs := telegramRuntimeJobs{
		scheduler:       scheduler,
		entryIDs:        make([]cron.EntryID, 0, 3),
		reportScheduler: jobs.NewTelegramReportScheduler(scheduler),
		backupScheduler: jobs.NewTelegramBackupScheduler(scheduler, runtimeAdapter{Runtime: runtime}),
	}
	runtimeJobs.reportScheduler.Run()
	runtimeJobs.backupScheduler.Run()
	for _, job := range []struct {
		spec string
		job  cron.Job
	}{
		{spec: "@every 12s", job: jobs.NewCPUAlertJob()},
		{spec: "@every 1m", job: runtimeJobs.reportScheduler},
		{spec: "@every 1m", job: runtimeJobs.backupScheduler},
	} {
		if err := runtimeJobs.add(job.spec, job.job); err != nil {
			runtimeJobs.stop()
			return telegramRuntimeJobs{}, err
		}
	}
	return runtimeJobs, nil
}

func (j *telegramRuntimeJobs) add(spec string, job cron.Job) error {
	entryID, err := j.scheduler.AddJob(spec, job)
	if err != nil {
		return err
	}
	j.entryIDs = append(j.entryIDs, entryID)
	return nil
}

func (j telegramRuntimeJobs) stop() {
	if j.reportScheduler != nil {
		j.reportScheduler.Stop()
	}
	if j.backupScheduler != nil {
		j.backupScheduler.Stop()
	}
	if j.scheduler == nil {
		return
	}
	for i := len(j.entryIDs) - 1; i >= 0; i-- {
		j.scheduler.RemoveJob(j.entryIDs[i])
	}
}

func callUnregisters(unregisters ...func()) {
	for _, unregister := range unregisters {
		if unregister != nil {
			unregister()
		}
	}
}

func telegramDeps(host componenthost.APIDeps) telegramhttp.Deps {
	return telegramhttp.Deps{
		Telegram: &telegramservice.Service{
			Runtime:  runtimeAdapter{Runtime: host.Runtime},
			Settings: telegramsettings.Reader{},
		},
		Settings:       telegramsettings.Reader{},
		AuditService:   service.AuditService{Runtime: host.Runtime},
		RequireScope:   host.Auth.RequireScope,
		Actor:          host.Request.Actor,
		RemoteIP:       host.Request.RemoteIP,
		CheckRateLimit: host.Rate.CheckRateLimit,
		Audit:          host.Audit.Audit,
		JSONObj:        host.HTTP.JSONObj,
	}
}

func newTelegramNotifier(runtime *service.Runtime) *telegramservice.Notifier {
	return telegramservice.NewNotifier(
		telegramservice.QueueCapacity,
		func(text string) telegramservice.Result {
			return (&telegramservice.Service{
				Runtime:  runtimeAdapter{Runtime: runtime},
				Settings: telegramsettings.Reader{},
			}).Send(text)
		},
		recordTelegramNotifierAudit,
	)
}

type runtimeAdapter struct {
	Runtime *service.Runtime
}

func (a runtimeAdapter) CoreHTTPClient(tag string, timeout time.Duration) (*http.Client, error) {
	return service.NewOutboundHTTPClientForRuntime(a.Runtime, tag, timeout)
}

func recordTelegramNotifierAudit(event string, details map[string]any) {
	if err := (&service.AuditService{}).Record(service.AuditEvent{
		Event:    event,
		Resource: "notifier",
		Severity: service.AuditSeverityWarn,
		Details:  details,
	}); err != nil {
		logger.Warning("telegram notifier audit failed:", err)
	}
}
