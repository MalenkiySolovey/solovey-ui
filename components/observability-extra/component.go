//go:build !minimal

package observabilityextra

import (
	"context"
	_ "embed"
	"errors"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	observabilityhttp "github.com/MalenkiySolovey/solovey-ui/components/observability-extra/api"
	"github.com/MalenkiySolovey/solovey-ui/components/observability-extra/jobs"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"

	"github.com/robfig/cron/v3"
)

//go:embed component.json
var componentJSON []byte

var componentManifest = manifest.MustFromJSON(componentJSON)
var id = componentManifest.ID

func init() {
	registry.Register(registry.Component{
		Manifest:      componentManifest,
		PortLifecycle: &component{},
	})
	registry.RegisterPortAPIRoutes(id, componenthost.PortRouteAdapter[observabilityhttp.Deps]{
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

func (c *component) Start(_ context.Context, ports componenthost.Ports) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return nil
	}
	if ports.Scheduler == nil {
		return errors.New("observability-extra scheduler is unavailable")
	}
	if ports.ObservationSampler == nil {
		return errors.New("observability-extra sampling owner is unavailable")
	}
	entryID, err := ports.Scheduler.AddJob("@every 2s", jobs.NewSamplingJob(ports.ObservationSampler))
	if err != nil {
		return err
	}
	c.started = true
	c.scheduler = ports.Scheduler
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

func observabilityExtraDeps(host componenthost.Ports) observabilityhttp.Deps {
	return observabilityhttp.Deps{
		ObservabilityService:   host.ObservationHistory,
		AuditService:           host.AuditHistory,
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
