//go:build !minimal

package observabilityextra

import (
	"context"
	"sync"

	canonical "github.com/MalenkiySolovey/solovey-component-observability-extra"
	hostports "github.com/MalenkiySolovey/solovey-host/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	observabilityhttp "github.com/MalenkiySolovey/solovey-ui/components/observability-extra/api"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service/observability"
)

var componentManifest = canonical.Manifest()
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

// component binds only the Panel database hook to the external lifecycle.
type component struct {
	mu          sync.Mutex
	feature     *canonical.Component
	unbindReset func()
}

func (c *component) Start(ctx context.Context, ports hostports.Ports) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.feature == nil {
		c.feature = canonical.New(logger.Warning)
	}
	if err := c.feature.Start(ctx, ports); err != nil {
		return err
	}
	if c.unbindReset == nil {
		c.unbindReset = observability.BindDatabaseReset()
	}
	return nil
}
func (c *component) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.feature != nil {
		if err := c.feature.Stop(ctx); err != nil {
			return err
		}
	}
	if c.unbindReset != nil {
		c.unbindReset()
		c.unbindReset = nil
	}
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
