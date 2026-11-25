//go:build !minimal

// Package blockcheck binds the optional feature to the local Product owners.
package blockcheck

import (
	feature "github.com/MalenkiySolovey/solovey-component-blockcheck"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/gin-gonic/gin"
)

func init() {
	component := new(feature.Component)
	registry.Register(registry.Component{Manifest: feature.Manifest(), PortLifecycle: component})
	registry.RegisterPortAPIRoutes(feature.ID, func(group *gin.RouterGroup, ports componenthost.Ports) error {
		registerRoutes(group, ports, component, localProvider{})
		return nil
	})
}
