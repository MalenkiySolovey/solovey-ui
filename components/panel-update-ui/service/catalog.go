package service

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	componentprofile "github.com/MalenkiySolovey/solovey-ui/internal/components/profile"
)

const ReleaseManifestFileEnv = "SUI_COMPONENT_RELEASE_MANIFEST_FILE"

type Catalog struct {
	ReleaseManifestFile string
}

func NewCatalog() Catalog {
	return Catalog{ReleaseManifestFile: os.Getenv(ReleaseManifestFileEnv)}
}

func (c Catalog) Inventory() (Inventory, error) {
	registered := registeredManifests()
	statuses, err := statusesForManifests(registered)
	if err != nil {
		return Inventory{}, err
	}
	release, err := c.loadReleaseCatalog()
	if err != nil {
		return Inventory{}, err
	}

	inventory := Inventory{BinaryProfile: componentprofile.Binary}
	seen := make(map[string]struct{}, len(statuses))
	for _, status := range statuses {
		seen[status.ID] = struct{}{}
		if releaseComponent, ok := release.Components[status.ID]; ok {
			mergeReleaseMetadata(&status, releaseComponent)
		}
		if status.Installed {
			status.Group = GroupInstalled
			inventory.Installed = append(inventory.Installed, status)
		} else {
			status.Group = GroupAvailable
			inventory.Available = append(inventory.Available, status)
		}
		inventory.Components = append(inventory.Components, status)
	}

	for id, releaseComponent := range release.Components {
		if _, ok := seen[id]; ok {
			continue
		}
		status := unavailableStatus(id, releaseComponent)
		inventory.Unavailable = append(inventory.Unavailable, status)
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
	for _, status := range inventory.Unavailable {
		if status.ID == id {
			return status, nil
		}
	}
	return ComponentStatus{}, fmt.Errorf("component is not available in this profile: %s", id)
}

func (c Catalog) loadReleaseCatalog() (releaseCatalog, error) {
	path := strings.TrimSpace(c.ReleaseManifestFile)
	if path == "" {
		return releaseCatalog{Components: map[string]releaseComponent{}}, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- update component reads the installer-provided release manifest path.
	if err != nil {
		return releaseCatalog{}, err
	}
	var catalog releaseCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return releaseCatalog{}, err
	}
	if catalog.Components == nil {
		catalog.Components = map[string]releaseComponent{}
	}
	return catalog, nil
}

type releaseCatalog struct {
	Components map[string]releaseComponent `json:"components"`
}

type releaseComponent struct {
	Name           string            `json:"name"`
	Version        string            `json:"version"`
	Since          string            `json:"since"`
	Delivery       manifest.Delivery `json:"delivery"`
	DefaultEnabled bool              `json:"defaultEnabled"`
	TokenScopes    []string          `json:"tokenScopes"`
}

func mergeReleaseMetadata(status *ComponentStatus, release releaseComponent) {
	if release.Name != "" {
		status.Name = release.Name
	}
	if release.Version != "" {
		status.LatestVersion = release.Version
	}
	if release.Since != "" {
		status.Since = release.Since
	}
	if release.Delivery != "" {
		status.Delivery = release.Delivery
	}
	if len(release.TokenScopes) > 0 {
		status.TokenScopes = append([]string(nil), release.TokenScopes...)
	}
}

func unavailableStatus(id string, release releaseComponent) ComponentStatus {
	name := strings.TrimSpace(release.Name)
	if name == "" {
		name = id
	}
	delivery := release.Delivery
	if delivery == "" {
		delivery = manifest.DeliveryInProcess
	}
	return ComponentStatus{
		ID:                id,
		Name:              name,
		Version:           release.Version,
		LatestVersion:     release.Version,
		Since:             release.Since,
		Delivery:          delivery,
		DefaultEnabled:    release.DefaultEnabled,
		TokenScopes:       append([]string(nil), release.TokenScopes...),
		AvailableInBinary: false,
		Installable:       false,
		Removable:         false,
		Installed:         false,
		Enabled:           false,
		Active:            false,
		Group:             GroupUnavailable,
		UnavailableReason: "not bundled in this binary profile",
	}
}

func sortInventory(inventory *Inventory) {
	sortStatuses(inventory.Components)
	sortStatuses(inventory.Installed)
	sortStatuses(inventory.Available)
	sortStatuses(inventory.Unavailable)
}

func sortStatuses(items []ComponentStatus) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
}
