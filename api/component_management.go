package api

import (
	"errors"

	"github.com/MalenkiySolovey/solovey-backend/componentapi"
	"github.com/MalenkiySolovey/solovey-backend/componentview"
	host "github.com/MalenkiySolovey/solovey-host/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/composition/componentmanagement"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func (a *ApiService) registerPanelComponentRoutes(g *gin.RouterGroup, requireStepUp func(*gin.Context, string, string) bool) {
	owner := componentManagerHTTP{settings: a.ConfigService, owner: componentmanagement.New(a.DataLifecycle)}
	componentapi.RegisterRoutes(g, componentapi.Deps{
		Components:       owner,
		ComponentManager: owner,
		LoginUser:        requestActor,
		RemoteIP:         getRemoteIp,
		Hostname:         getHostname,
		RequireScope:     a.requireTokenScopeAny,
		RequireStepUp:    requireStepUp,
		ActionPolicy:     a.actionPolicyFor(requireStepUp, true),
		Audit:            a.recordAudit,
		JSONObj:          jsonObj,
		JSONMsg:          jsonMsg,
	})
}

// This is composition: ComponentHost owns the rules, installstate writes the
// metadata, and Backend authenticates the caller and projects HTTP results.
type componentManagerHTTP struct {
	owner    host.Manager
	settings *service.ConfigService
}

func (a componentManagerHTTP) Inventory() (componentview.Inventory, error) {
	inventory, err := a.owner.Inventory()
	if err != nil {
		return componentview.Inventory{}, err
	}
	return componentview.Inventory{BinaryProfile: inventory.BinaryProfile, Components: componentStatusesHTTP(inventory.Components),
		Installed: componentStatusesHTTP(inventory.Installed), Available: componentStatusesHTTP(inventory.Available), Unavailable: componentStatusesHTTP(inventory.Unavailable)}, nil
}

func (a componentManagerHTTP) Enable(operation componentview.OperationContext, id string) (componentview.ComponentStatus, error) {
	return a.setEnabled(operation, id, true)
}

func (a componentManagerHTTP) Disable(operation componentview.OperationContext, id string) (componentview.ComponentStatus, error) {
	return a.setEnabled(operation, id, false)
}

func (a componentManagerHTTP) setEnabled(operation componentview.OperationContext, id string, enabled bool) (componentview.ComponentStatus, error) {
	if a.settings == nil {
		return componentview.ComponentStatus{}, errors.New("component settings authority is unavailable")
	}
	status, err := a.owner.SetEnabled(operation.RequestContext, id, enabled, func(value bool) error {
		return a.settings.SetComponentEnabled(id, value, operation.Actor, operation.Hostname)
	})
	return componentStatusHTTP(status), err
}

func (a componentManagerHTTP) Install(operation componentview.OperationContext, id string) (componentview.ComponentStatus, error) {
	status, err := a.owner.Install(operation.RequestContext, id)
	return componentStatusHTTP(status), err
}

func (a componentManagerHTTP) Remove(operation componentview.OperationContext, id string) (componentview.ComponentStatus, error) {
	status, err := a.owner.Remove(operation.RequestContext, id)
	return componentStatusHTTP(status), err
}

func componentStatusesHTTP(statuses []host.ComponentStatus) []componentview.ComponentStatus {
	if statuses == nil {
		return nil
	}
	result := make([]componentview.ComponentStatus, len(statuses))
	for i, status := range statuses {
		result[i] = componentStatusHTTP(status)
	}
	return result
}

func componentStatusHTTP(status host.ComponentStatus) componentview.ComponentStatus {
	return componentview.ComponentStatus{ID: status.ID, Name: status.Name, Version: status.Version, LatestVersion: status.LatestVersion,
		Since: status.Since, RequiredPanel: status.RequiredPanel, Delivery: status.Delivery, DefaultEnabled: status.DefaultEnabled,
		TokenScopes: status.TokenScopes, AvailableInBinary: status.AvailableInBinary, Compatible: status.Compatible,
		Locked: status.Locked, LockedReason: status.LockedReason, Installable: status.Installable, Removable: status.Removable,
		Installed: status.Installed, Enabled: status.Enabled, Active: status.Active,
		ObservedLifecycle: componentview.LifecycleObservation(status.ObservedLifecycle), Group: componentview.Group(status.Group), UnavailableReason: status.UnavailableReason}
}
