// Package componenthttp binds Product-selected Component owners to Backend HTTP inputs.
package componenthttp

import (
	"github.com/MalenkiySolovey/solovey-ui/backend/actions"
	"github.com/MalenkiySolovey/solovey-ui/backend/componentroutes"
	"github.com/MalenkiySolovey/solovey-ui/backend/componentview"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
	componentprofile "github.com/MalenkiySolovey/solovey-ui/internal/components/profile"
	"github.com/gin-gonic/gin"
)

type Adapter struct{ owners componenthost.APIDeps }

// New accepts the already-bound Product Component owners. Backend never receives
// this legacy Product composition object or its Runtime.
func New(owners componenthost.APIDeps) *Adapter { return &Adapter{owners: owners} }

var _ componentroutes.Composition = (*Adapter)(nil)

func (a *Adapter) Routes(ports componentroutes.Ports) ([]componentroutes.Route, error) {
	if a == nil {
		return nil, componentroutes.ErrUnavailable
	}
	state.InvalidateActiveCache()
	installed, err := state.InstalledIDs()
	if err != nil {
		return nil, err
	}
	deps := a.owners
	deps.ComponentSettings = ports.ComponentSettings
	deps.Auth, deps.Request, deps.Rate = ports.Auth, ports.Request, ports.Rate
	deps.Audit, deps.HTTP, deps.Update = ports.Audit, ports.HTTP, ports.Update

	registrars := registry.APIRouteRegistrarsByComponentIDs(installed)
	result := make([]componentroutes.Route, 0, len(registrars))
	for _, registrar := range registrars {
		result = append(result, componentroutes.Route{ComponentID: registrar.ComponentID, Register: func(g *gin.RouterGroup) error { return registrar.Register(g, deps) }})
	}
	return result, nil
}

func (a *Adapter) IsActive(id string) (bool, error) {
	if a == nil {
		return false, componentroutes.ErrUnavailable
	}
	return state.IsActiveCached(id)
}

func (a *Adapter) DeclaresAction(componentID, actionID string) bool {
	if a == nil {
		return false
	}
	component, registered := registry.ComponentByID(componentID)
	return registered && actions.ComponentAllowed(componentID, component.Manifest.Actions, actionID)
}

func (a *Adapter) Inventory() (componentview.RuntimeInventory, error) {
	if a == nil {
		return componentview.RuntimeInventory{}, componentroutes.ErrUnavailable
	}
	components, err := state.Components()
	return projectInventory(components), err
}

func projectInventory(components []state.Component) componentview.RuntimeInventory {
	result := componentview.RuntimeInventory{BinaryProfile: componentprofile.Binary}
	if components != nil {
		result.Components = make([]componentview.RuntimeComponent, 0, len(components))
	}
	for _, component := range components {
		observation := component.ObservedLifecycle
		result.Components = append(result.Components, componentview.RuntimeComponent{
			ID: component.ID, Name: component.Name, Version: component.Version, Since: component.Since, Delivery: component.Delivery,
			DefaultEnabled: component.DefaultEnabled, TokenScopes: append([]string(nil), component.TokenScopes...),
			Installed: component.Installed, Enabled: component.Enabled, Active: component.Active, AvailableInBinary: component.AvailableInBinary,
			ObservedLifecycle: componentview.LifecycleObservation{Phase: observation.Phase, InstanceRetention: observation.InstanceRetention, Source: observation.Source, ObservedAt: observation.ObservedAt, ReasonCode: observation.ReasonCode},
		})
	}
	return result
}

func (a *Adapter) Owners() ([]componentroutes.OwnerAvailability, error) {
	if a == nil {
		return nil, componentroutes.ErrUnavailable
	}
	installed, err := installstate.InstalledComponents()
	if err != nil {
		return nil, err
	}
	available := map[string]registry.Component{}
	for _, component := range registry.Components() {
		available[component.Manifest.ID] = component
	}
	result := make([]componentroutes.OwnerAvailability, 0, len(installed))
	for _, component := range installed {
		owner, present := available[component.ID]
		status := componentroutes.OwnerAvailability{ID: component.ID, Available: present}
		if present {
			enabled, enabledErr := enabledstate.Enabled(owner.Manifest)
			status.Enabled, status.EnabledUnavailable = enabled, enabledErr != nil
		}
		result = append(result, status)
	}
	return result, nil
}
