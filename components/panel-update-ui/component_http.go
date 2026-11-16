//go:build !minimal

package panelupdateui

import (
	"github.com/MalenkiySolovey/solovey-ui/backend/componentview"
	panelupdateservice "github.com/MalenkiySolovey/solovey-ui/components/panel-update-ui/service"
)

// componentManagerHTTP projects the existing lifecycle owner into Backend HTTP.
// It does not register components or execute an independent lifecycle.
type componentManagerHTTP struct{ owner panelupdateservice.Manager }

func (a componentManagerHTTP) Inventory() (componentview.Inventory, error) {
	inventory, err := a.owner.Inventory()
	if err != nil {
		return componentview.Inventory{}, err
	}
	return componentview.Inventory{BinaryProfile: inventory.BinaryProfile, Components: componentStatusesHTTP(inventory.Components),
		Installed: componentStatusesHTTP(inventory.Installed), Available: componentStatusesHTTP(inventory.Available), Unavailable: componentStatusesHTTP(inventory.Unavailable)}, nil
}

func (a componentManagerHTTP) Enable(operation componentview.OperationContext, id string) (componentview.ComponentStatus, error) {
	status, err := a.owner.Enable(panelupdateservice.OperationContext(operation), id)
	return componentStatusHTTP(status), err
}
func (a componentManagerHTTP) Disable(operation componentview.OperationContext, id string) (componentview.ComponentStatus, error) {
	status, err := a.owner.Disable(panelupdateservice.OperationContext(operation), id)
	return componentStatusHTTP(status), err
}
func (a componentManagerHTTP) Install(operation componentview.OperationContext, id string) (componentview.ComponentStatus, error) {
	status, err := a.owner.Install(panelupdateservice.OperationContext(operation), id)
	return componentStatusHTTP(status), err
}
func (a componentManagerHTTP) Remove(operation componentview.OperationContext, id string) (componentview.ComponentStatus, error) {
	status, err := a.owner.Remove(panelupdateservice.OperationContext(operation), id)
	return componentStatusHTTP(status), err
}
func componentStatusesHTTP(statuses []panelupdateservice.ComponentStatus) []componentview.ComponentStatus {
	if statuses == nil {
		return nil
	}
	result := make([]componentview.ComponentStatus, len(statuses))
	for i, status := range statuses {
		result[i] = componentStatusHTTP(status)
	}
	return result
}
func componentStatusHTTP(status panelupdateservice.ComponentStatus) componentview.ComponentStatus {
	return componentview.ComponentStatus{ID: status.ID, Name: status.Name, Version: status.Version, LatestVersion: status.LatestVersion,
		Since: status.Since, RequiredPanel: status.RequiredPanel, Delivery: status.Delivery, DefaultEnabled: status.DefaultEnabled,
		TokenScopes: status.TokenScopes, AvailableInBinary: status.AvailableInBinary, Compatible: status.Compatible,
		Locked: status.Locked, LockedReason: status.LockedReason, Installable: status.Installable, Removable: status.Removable,
		Installed: status.Installed, Enabled: status.Enabled, Active: status.Active,
		ObservedLifecycle: componentview.LifecycleObservation(status.ObservedLifecycle), Group: componentview.Group(status.Group), UnavailableReason: status.UnavailableReason}
}
