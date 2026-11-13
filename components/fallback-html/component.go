//go:build !minimal

package fallbackhtml

import (
	"context"

	canonical "github.com/MalenkiySolovey/solovey-component-fallback-html"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	fallbackapi "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/api"
	fallbacklifecycle "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"
)

var componentJSON = canonical.ManifestJSON()

var componentManifest = manifest.MustFromJSON(componentJSON)
var id = componentManifest.ID

func init() {
	binding, err := durableowner.Bind(componentManifest, fallbacklifecycle.Owner{}, durableowner.BackendAdapter{
		Migrate: backendMigrate, Restore: backendRestore, Inspect: backendInspect, Drop: backendDrop,
	})
	if err != nil {
		panic(err)
	}
	registry.Register(registry.Component{Manifest: componentManifest, Lifecycle: component{}, Durable: binding})
	registry.RegisterAPIRoutes(id, componenthost.RouteAdapter[fallbackapi.Deps]{Build: fallbackDeps, Register: fallbackapi.RegisterRoutes}.RegisterRoutes)
}

type component struct{}

func (component) Start(ctx context.Context, _ lifecycle.Context) error { return startBackend(ctx) }
func (component) Stop(ctx context.Context) error                       { return stopBackend(ctx) }
