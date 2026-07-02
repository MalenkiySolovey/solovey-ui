//go:build !minimal

package observabilityextra

import (
	"context"
	_ "embed"
	"sync"

	telemetryhttp "github.com/MalenkiySolovey/solovey-ui/api/telemetry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	observabilityhttp "github.com/MalenkiySolovey/solovey-ui/components/observability-extra/api"
	"github.com/MalenkiySolovey/solovey-ui/components/observability-extra/jobs"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/robfig/cron/v3"
)

//go:embed component.json
var componentJSON []byte

var componentManifest = manifest.MustFromJSON(componentJSON)
var id = componentManifest.ID

func init() {
	registry.Register(registry.Component{
		Manifest:  componentManifest,
		Lifecycle: &component{},
	})
	registry.RegisterAPIRoutes(id, componenthost.RouteAdapter[observabilityhttp.Deps]{
		Build:    observabilityExtraDeps,
		Register: observabilityhttp.RegisterRoutes,
	}.RegisterRoutes)
}

type component struct {
	mu        sync.Mutex
	scheduler componenthost.Scheduler
	entryID   cron.EntryID
}

func (c *component) Start(_ context.Context, ctx lifecycle.Context) error {
	if ctx.Host.Scheduler == nil {
		return nil
	}
	entryID, err := ctx.Host.Scheduler.AddJob("@every 2s", jobs.NewSamplingJob())
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.scheduler = ctx.Host.Scheduler
	c.entryID = entryID
	c.mu.Unlock()
	return nil
}

func (c *component) Stop(context.Context) error {
	c.mu.Lock()
	scheduler := c.scheduler
	entryID := c.entryID
	c.scheduler = nil
	c.entryID = 0
	c.mu.Unlock()
	if scheduler != nil && entryID != 0 {
		scheduler.RemoveJob(entryID)
	}
	return nil
}

func observabilityExtraDeps(host componenthost.APIDeps) observabilityhttp.Deps {
	return observabilityhttp.Deps{
		Telemetry: telemetryhttp.Deps{
			StatsService:           service.StatsService{Runtime: host.Runtime},
			ServerService:          service.NewServerService(host.Runtime),
			DiagnosticsService:     service.DiagnosticsService{Runtime: host.Runtime},
			DoctorService:          service.DoctorService{Runtime: host.Runtime},
			AuditService:           service.AuditService{Runtime: host.Runtime},
			VersionService:         service.VersionService{},
			RequireScope:           host.Auth.RequireScope,
			JSONObj:                host.HTTP.JSONObj,
			JSONMsg:                host.HTTP.JSONMsg,
			Hostname:               host.Request.Hostname,
			ValidateTarget:         host.Request.ValidateTarget,
			Audit:                  host.Audit.Audit,
			LoginUser:              host.Auth.LoginUser,
			RequireAuditAdminScope: host.Auth.RequireAuditAdminScope,
			Actor:                  host.Request.Actor,
			RemoteIP:               host.Request.RemoteIP,
			CheckAuditRateLimit:    host.Rate.CheckAuditRateLimit,
			AuditRateLimitKey:      host.Rate.AuditRateLimitKey,
			AuditRateLimitWindow:   host.Rate.AuditRateLimitWindow,
		},
		ObservabilityService: service.ObservabilityService{ServerService: service.NewServerService(host.Runtime)},
	}
}
