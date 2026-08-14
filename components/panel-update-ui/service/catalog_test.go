//go:build !minimal

package service

import (
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestCatalogProjectsOnlyComponentsInRunningBinary(t *testing.T) {
	const componentID = "test-catalog-component"
	registerCatalogTestComponent(componentID)
	installedPath := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := installstate.Store(installedPath, installstate.Metadata{
		Version: 1,
		Profile: "full",
		Binary:  "full",
		Components: []installstate.InstalledComponent{
			{ID: componentID, Delivery: manifest.DeliveryInProcess, Installed: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	inventory, err := NewCatalog().Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Unavailable) != 0 {
		t.Fatalf("UI catalog must not project a second release authority: %#v", inventory)
	}
	found := false
	for _, status := range inventory.Installed {
		if status.ID != componentID {
			continue
		}
		found = true
		if status.Version != "1" || status.LatestVersion != "1" || !status.AvailableInBinary {
			t.Fatalf("registered manifest was not projected exactly: %#v", status)
		}
	}
	if !found {
		t.Fatalf("installed component missing from catalog: %#v", inventory.Installed)
	}
}

func registerCatalogTestComponent(id string) {
	if _, exists := registry.ComponentByID(id); exists {
		return
	}
	registry.Register(registry.Component{
		Manifest: manifest.Manifest{
			ID: id, Name: "Catalog Test Component", Version: "1",
			Delivery: manifest.DeliveryInProcess, DefaultEnabled: true,
			TokenScopes: []string{"test-catalog"},
		},
		Lifecycle: lifecycle.Noop{},
	})
}
