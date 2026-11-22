//go:build !minimal

// Package zapret binds the independent component to the Panel registry.
package zapret

import (
	feature "github.com/MalenkiySolovey/solovey-component-zapret"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
)

func init() {
	registry.Register(registry.Component{Manifest: feature.Manifest(), PortLifecycle: feature.Component{}})
	registry.RegisterPortAPIRoutes("zapret", componenthost.PortRouteAdapter[componenthost.Ports]{
		Build:    func(ports componenthost.Ports) componenthost.Ports { return ports },
		Register: registerRoutes,
	}.RegisterRoutes)
}
