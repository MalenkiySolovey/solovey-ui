//go:build !minimal

package app

import (
	"github.com/MalenkiySolovey/solovey-host/observation"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

// Full-profile builds generate the selected component imports and optional
// observation owner constructor in app/components_generated.go.
var _ = soloveyGeneratedComponentImports

type observationOwner interface {
	observation.Sampler
	observation.History
}

// WithSelectedObservationOwners binds the Panel owners only when selected.
func WithSelectedObservationOwners(owners componenthost.APIDeps) componenthost.APIDeps {
	if newSelectedObservationOwner == nil || owners.Runtime == nil {
		return owners
	}
	owner := newSelectedObservationOwner(service.NewServerService(owners.Runtime))
	owners.ObservationSampler, owners.ObservationHistory = owner, owner
	owners.AuditHistory = &service.AuditService{Runtime: owners.Runtime}
	return owners
}
