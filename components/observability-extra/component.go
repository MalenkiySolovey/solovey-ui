//go:build !minimal

package observabilityextra

import (
	"context"
	_ "embed"
	"errors"
	"sync"

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
	started   bool
	scheduler componenthost.Scheduler
	entryID   cron.EntryID
}

func (c *component) Start(_ context.Context, ctx lifecycle.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return nil
	}
	if ctx.Host.Scheduler == nil {
		return errors.New("observability-extra scheduler is unavailable")
	}
	sampler := &service.ObservabilityService{ServerService: service.NewServerService(ctx.Host.API.Runtime)}
	entryID, err := ctx.Host.Scheduler.AddJob("@every 2s", jobs.NewSamplingJob(sampler))
	if err != nil {
		return err
	}
	c.started = true
	c.scheduler = ctx.Host.Scheduler
	c.entryID = entryID
	return nil
}

func (c *component) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	scheduler := c.scheduler
	entryID := c.entryID
	if scheduler != nil && entryID != 0 {
		if err := scheduler.RemoveJobAndWait(ctx, entryID); err != nil {
			return err
		}
	}
	c.started = false
	c.scheduler = nil
	c.entryID = 0
	return nil
}

func observabilityExtraDeps(host componenthost.APIDeps) observabilityhttp.Deps {
	return observabilityhttp.Deps{
		ObservabilityService:   &service.ObservabilityService{ServerService: service.NewServerService(host.Runtime)},
		AuditService:           &service.AuditService{Runtime: host.Runtime},
		RequireScope:           host.Auth.RequireScope,
		RequireAuditAdminScope: host.Auth.RequireAuditAdminScope,
		JSONObj:                host.HTTP.JSONObj,
		Actor:                  host.Request.Actor,
		RemoteIP:               host.Request.RemoteIP,
		CheckAuditRateLimit:    host.Rate.CheckAuditRateLimit,
		AuditRateLimitKey:      host.Rate.AuditRateLimitKey,
		AuditRateLimitWindow:   host.Rate.AuditRateLimitWindow,
		Audit:                  host.Audit.Audit,
	}
}
