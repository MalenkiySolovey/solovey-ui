//go:build !minimal

package service

import (
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
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

func TestCatalogKeepsInstalledOwnerAbsentFromBinaryNonActionable(t *testing.T) {
	state.NewLifecyclePublisher()
	const id = "test-catalog-absent-owner"
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	if err := installstate.Store(path, installstate.Metadata{Version: 1, Binary: "full", Components: []installstate.InstalledComponent{
		{ID: id, Delivery: manifest.DeliveryInProcess, Installed: true},
	}}); err != nil {
		t.Fatal(err)
	}
	inventory, err := NewCatalog().Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Unavailable) != 1 || len(inventory.Installed) != 0 {
		t.Fatalf("unavailable owner missing or loadable: %+v", inventory)
	}
	status := inventory.Unavailable[0]
	if status.ID != id || !status.Installed || status.AvailableInBinary || status.Active || status.Enabled || status.Installable || status.Removable || status.Compatible || status.Version != "" || status.LatestVersion != "" || status.ObservedLifecycle.Phase != state.LifecycleBinaryAbsent {
		t.Fatalf("absent owner acquired unsupported authority: %+v", status)
	}
}

func TestCatalogLifecycleDoesNotReplaceEligibility(t *testing.T) {
	publisher := state.NewLifecyclePublisher()
	item := manifest.Manifest{ID: "test-catalog-observed", Version: "1"}
	publisher.RecordStart(item.ID, false)
	status := statusForManifest(item, true, true)
	if !status.Active || !status.Enabled || status.ObservedLifecycle.Phase != state.LifecycleStartFailed {
		t.Fatalf("failed Start changed legacy eligibility: %+v", status)
	}
	publisher.RecordStop(item.ID, false)
	status = statusForManifest(item, false, false)
	if status.Installed || status.Active || status.ObservedLifecycle.Phase != state.LifecycleStopFailed || status.ObservedLifecycle.InstanceRetention != state.InstanceRetained {
		t.Fatalf("removed metadata hid failed Stop: %+v", status)
	}

	state.NewLifecyclePublisher()
	status = statusForManifest(item, false, false)
	if status.ObservedLifecycle.Phase != state.LifecycleNotInstalled || status.ObservedLifecycle.InstanceRetention != state.InstanceUnknown {
		t.Fatalf("missing current observation invented release: %+v", status)
	}
}
