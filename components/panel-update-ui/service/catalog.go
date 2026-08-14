package service

import (
	"fmt"
	"sort"
	"strings"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	componentprofile "github.com/MalenkiySolovey/solovey-ui/internal/components/profile"
)

// Catalog is a read-only projection of components already compiled into the
// running binary. Signed release discovery and artifact installation belong to
// the core update lifecycle, not to this UI component.
type Catalog struct{}

func NewCatalog() Catalog { return Catalog{} }

func (Catalog) Inventory() (Inventory, error) {
	statuses, err := statusesForManifests(registeredManifests())
	if err != nil {
		return Inventory{}, err
	}
	inventory := Inventory{
		BinaryProfile: componentprofile.Binary,
		Components:    append([]ComponentStatus(nil), statuses...),
		Unavailable:   []ComponentStatus{},
	}
	for _, status := range statuses {
		if status.Installed {
			status.Group = GroupInstalled
			inventory.Installed = append(inventory.Installed, status)
			continue
		}
		status.Group = GroupAvailable
		inventory.Available = append(inventory.Available, status)
	}
	sortInventory(&inventory)
	return inventory, nil
}

func (c Catalog) StatusByID(id string) (ComponentStatus, error) {
	if err := manifest.ValidateID(id); err != nil {
		return ComponentStatus{}, err
	}
	inventory, err := c.Inventory()
	if err != nil {
		return ComponentStatus{}, err
	}
	for _, status := range inventory.Components {
		if status.ID == id {
			return status, nil
		}
	}
	return ComponentStatus{}, fmt.Errorf("component is not available in this binary: %s", id)
}

func requiredPanelVersion(since string) string { return strings.TrimSpace(since) }

func panelVersionCompatible(required string) bool {
	required = strings.TrimSpace(required)
	if required == "" {
		return true
	}
	comparison, ok := versionpolicy.CompareVersions(configidentity.GetVersion(), required)
	return ok && comparison >= 0
}

func sortInventory(inventory *Inventory) {
	sortStatuses(inventory.Components)
	sortStatuses(inventory.Installed)
	sortStatuses(inventory.Available)
	sortStatuses(inventory.Unavailable)
}

func sortStatuses(items []ComponentStatus) {
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
}
