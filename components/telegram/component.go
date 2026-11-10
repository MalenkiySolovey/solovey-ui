//go:build !minimal

package telegram

import (
	"context"
	"errors"
	"strings"
	"sync"

	canonical "github.com/MalenkiySolovey/solovey-component-telegram"
	hostports "github.com/MalenkiySolovey/solovey-host/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	telegramhttp "github.com/MalenkiySolovey/solovey-ui/components/telegram/api"
	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/components/telegram/jobs"
	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/diagnostics"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

var telegramManifest = canonical.Manifest()
var id = telegramManifest.ID

func init() {
	registry.Register(registry.Component{
		Manifest:      telegramManifest,
		PortLifecycle: &component{},
	})
	registry.RegisterPortAPIRoutes(id, componenthost.PortRouteAdapter[telegramhttp.Deps]{
		Build:    telegramDeps,
		Register: telegramhttp.RegisterRoutes,
	}.RegisterRoutes)
}

type component struct {
	mu                     sync.Mutex
	started                bool
	scheduledJobs          *canonical.ScheduledJobs
	unregisterEvent        func()
	unregisterContribution func()
	unregisterBackupCodecs func()
	unregisterSettings     func()
	unregisterLogCategory  func()
	unregisterTokenScope   func()
	notifier               *telegramservice.Notifier
}

func (c *component) Start(ctx context.Context, ports hostports.Ports) error {
	return c.start(ctx, ports.Scheduler, ports.OutboundHTTP, ports.AuditRecorder)
}

func (c *component) start(ctx context.Context, scheduler componenthost.Scheduler, transport componenthost.OutboundHTTPClientProvider, audit componenthost.AuditRecorder) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return nil
	}
	if scheduler == nil {
		c.mu.Unlock()
		return errors.New("telegram scheduler is unavailable")
	}
	if transport == nil || audit == nil {
		c.mu.Unlock()
		return errors.New("telegram transport or audit port is unavailable")
	}
	c.started = true
	if c.notifier == nil {
		c.notifier = newTelegramNotifier(transport, audit)
	}
	notifier := c.notifier
	if c.unregisterEvent == nil {
		c.unregisterEvent = service.RegisterPanelEventNotifier(id, func(event string, fields map[string]string) {
			(&telegramservice.Service{
				Runtime:  transport,
				Notifier: notifier,
				Settings: telegramsettings.Reader{},
			}).NotifyEvent(event, fields)
		})
	}
	if c.unregisterContribution == nil {
		c.unregisterContribution = registerSettingContribution()
	}
	if c.unregisterBackupCodecs == nil {
		unregister, err := registerBackupCodecs()
		if err != nil {
			c.mu.Unlock()
			_ = c.Stop(ctx)
			return err
		}
		c.unregisterBackupCodecs = unregister
	}
	if c.unregisterSettings == nil {
		c.unregisterSettings = registerSettingsObserver(audit)
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
	runtimeJobs, err := canonical.StartScheduledJobs(scheduler, jobs.NewCPUAlertJob(), jobs.NewTelegramReportScheduler(scheduler), jobs.NewTelegramBackupScheduler(scheduler, transport, func(record telegramservice.AuditRecord) error { return recordTelegramBackupAudit(audit, record) }))
	if err != nil {
		c.mu.Unlock()
		_ = c.Stop(ctx)
		return err
	}

	c.scheduledJobs = runtimeJobs
	c.mu.Unlock()
	return nil
}

func (c *component) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	runtimeJobs := c.scheduledJobs
	unregisterEvent := c.unregisterEvent
	unregisterContribution := c.unregisterContribution
	unregisterBackupCodecs := c.unregisterBackupCodecs
	unregisterSettings := c.unregisterSettings
	unregisterLogCategory := c.unregisterLogCategory
	unregisterTokenScope := c.unregisterTokenScope
	notifier := c.notifier
	if err := runtimeJobs.Stop(ctx); err != nil {
		return err
	}

	if notifier != nil {
		if err := notifier.Stop(ctx); err != nil {
			return err
		}
	}

	callUnregisters(
		unregisterEvent,
		unregisterBackupCodecs,
		unregisterSettings,
		unregisterLogCategory,
		unregisterTokenScope,
		unregisterContribution,
	)
	telegramservice.ResetHTTPClient()

	c.started = false
	c.scheduledJobs = nil
	c.unregisterEvent = nil
	c.unregisterContribution = nil
	c.unregisterBackupCodecs = nil
	c.unregisterSettings = nil
	c.unregisterLogCategory = nil
	c.unregisterTokenScope = nil
	c.notifier = nil
	return nil
}

func (c *component) DropData(ctx context.Context, ports hostports.Ports) error {
	if ports.DropOwnedSettings == nil {
		return errors.New("telegram owned settings port is unavailable")
	}
	return ports.DropOwnedSettings(ctx)
}

func callUnregisters(unregisters ...func()) {
	for _, unregister := range unregisters {
		if unregister != nil {
			unregister()
		}
	}
}

func telegramDeps(host componenthost.Ports) telegramhttp.Deps {
	return telegramhttp.Deps{
		Telegram: &telegramservice.Service{
			Runtime:  host.OutboundHTTP,
			Settings: telegramsettings.Reader{},
		},
		Settings:       telegramsettings.Reader{},
		AuditService:   host.Audit.Recorder,
		RequireScope:   host.Auth.RequireScope,
		Actor:          host.Request.Actor,
		RemoteIP:       host.Request.RemoteIP,
		CheckRateLimit: host.Rate.CheckRateLimit,
		Audit:          host.Audit.Audit,
		JSONObj:        host.HTTP.JSONObj,
	}
}

func newTelegramNotifier(transport componenthost.OutboundHTTPClientProvider, audit componenthost.AuditRecorder) *telegramservice.Notifier {
	return telegramservice.NewNotifierContext(
		telegramservice.QueueCapacity,
		func(ctx context.Context, text string) telegramservice.Result {
			return (&telegramservice.Service{
				Runtime:  transport,
				Settings: telegramsettings.Reader{},
			}).SendContext(ctx, text)
		},
		func(event string, details map[string]any) {
			recordTelegramNotifierAudit(audit, event, details)
		},
	)
}

func recordTelegramNotifierAudit(audit componenthost.AuditRecorder, event string, details map[string]any) {
	if err := audit.Record(service.AuditEvent{
		Event:    event,
		Resource: "notifier",
		Severity: service.AuditSeverityWarn,
		Details:  details,
	}); err != nil {
		logger.Warning("telegram notifier audit failed:", err)
	}
}

func recordTelegramBackupAudit(audit componenthost.AuditRecorder, record telegramservice.AuditRecord) error {
	return audit.Record(service.AuditEvent{
		Actor:    record.Actor,
		Event:    record.Event,
		Resource: record.Resource,
		Severity: record.Severity,
		Details:  record.Details,
	})
}
