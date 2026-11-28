//go:build !minimal

// Package blockcheck binds the optional feature to the local Product owners.
package blockcheck

import (
	"github.com/MalenkiySolovey/solovey-backend/storage/durableowner"
	feature "github.com/MalenkiySolovey/solovey-component-blockcheck"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/gin-gonic/gin"
)

func init() {
	component := new(feature.Component)
	manifest := feature.Manifest()
	binding, err := durableowner.Bind(manifest, feature.CatalogOwner{}, durableowner.BackendAdapter{
		Migrate: migrateStorage, Restore: migrateStorage, Inspect: inspectCatalogDrop, Drop: dropCatalog,
	})
	if err != nil {
		panic(err)
	}
	registry.Register(registry.Component{Manifest: manifest, PortLifecycle: component, Durable: binding})
	registry.RegisterPortAPIRoutes(feature.ID, func(group *gin.RouterGroup, ports componenthost.Ports) error {
		registerRoutes(group, ports, component, localProvider{})
		return nil
	})
}
