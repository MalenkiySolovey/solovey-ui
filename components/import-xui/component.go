//go:build !minimal

package importxui

import (
	"context"
	_ "embed"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	importxuihttp "github.com/MalenkiySolovey/solovey-ui/components/import-xui/api"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service"
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
	registry.RegisterAPIRoutes(id, componenthost.RouteAdapter[importxuihttp.Deps]{
		Build:    importXUIDeps,
		Register: importxuihttp.RegisterRoutes,
	}.RegisterRoutes)
}

type component struct {
	mu              sync.Mutex
	unregisterReset func()
}

func (c *component) Start(context.Context, lifecycle.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unregisterReset == nil {
		c.unregisterReset = importxuihttp.RegisterResetHook()
	}
	return nil
}

func (c *component) Stop(context.Context) error {
	c.mu.Lock()
	unregister := c.unregisterReset
	c.unregisterReset = nil
	c.mu.Unlock()
	if unregister != nil {
		unregister()
	}
	return nil
}

func importXUIDeps(host componenthost.APIDeps) importxuihttp.Deps {
	configService := service.NewConfigServiceWithRuntime(host.Runtime)
	return importxuihttp.Deps{
		AuditHistory:  &service.AuditService{Runtime: host.Runtime},
		RequireScope:  host.Auth.RequireScope,
		RequireStepUp: host.Auth.RequireStepUp,
		Audit:         host.Audit.Audit,
		Actor:         host.Request.Actor,
		RemoteIP:      host.Request.RemoteIP,
		Hostname:      host.Request.Hostname,
		JSONObj:       host.HTTP.JSONObj,
		JSONMsg:       host.HTTP.JSONMsg,
		ConfigChanged: func() {
			configService.ApplyComponentConfigChangeEffects(service.ComponentConfigChangeEffects{
				PrimaryObject:  "config",
				IncludeObjects: []string{"inbounds", "outbounds", "endpoints", "tls", "clients", "settings"},
				CoreRestart:    true,
				RestartReason:  "compatible panel import",
			})
		},
	}
}
