//go:build !minimal

// Package zapret binds the independent component to the Panel registry.
package zapret

import (
	"github.com/MalenkiySolovey/solovey-backend/storage/durableowner"
	feature "github.com/MalenkiySolovey/solovey-component-zapret"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
)

func init() {
	manifest := feature.Manifest()
	binding, err := durableowner.Bind(manifest, feature.CatalogOwner{}, durableowner.BackendAdapter{
		Migrate: migrateCatalog, Restore: migrateCatalog, Inspect: inspectCatalogDrop, Drop: dropCatalog,
	})
	if err != nil {
		panic(err)
	}
	registry.Register(registry.Component{Manifest: manifest, PortLifecycle: feature.Component{}, Durable: binding})
	registry.RegisterPortAPIRoutes("zapret", componenthost.PortRouteAdapter[componenthost.Ports]{
		Build:    func(ports componenthost.Ports) componenthost.Ports { return ports },
		Register: registerRoutes,
	}.RegisterRoutes)
}
