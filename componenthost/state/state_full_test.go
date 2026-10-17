//go:build !minimal

package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestComponentsEmptyWhenMetadataIsAbsent(t *testing.T) {
	t.Setenv(installstate.InstalledFileEnv, filepath.Join(t.TempDir(), "installed.json"))

	components, err := Components()
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 0 {
		t.Fatalf("expected no implicit components, got %d: %#v", len(components), components)
	}
}

func TestComponentsReturnOnlyInstalledMetadataEntries(t *testing.T) {
	registerStateTestComponent("test-state-installed")
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"binary": "full",
		"components": [
			{"id": "test-state-installed", "delivery": "in-process", "installed": true}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	components, err := Components()
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 1 || components[0].ID != "test-state-installed" {
		t.Fatalf("expected only installed test component, got %#v", components)
	}
	if !components[0].Installed || !components[0].Enabled || !components[0].Active {
		t.Fatalf("installed default-enabled component should be active: %#v", components[0])
	}
}

func registerStateTestComponent(id string) {
	if _, exists := registry.ComponentByID(id); exists {
		return
	}
	registry.Register(registry.Component{
		Manifest: manifest.Manifest{
			ID:             id,
			Name:           id,
			Version:        "1",
			Delivery:       manifest.DeliveryInProcess,
			DefaultEnabled: true,
		},
		Lifecycle: lifecycle.Noop{},
	})
}

func TestComponentsKeepDisabledAndBinaryAbsentOwnershipExplicit(t *testing.T) {
	NewLifecyclePublisher()
	const disabledID = "test-state-disabled"
	registry.Register(registry.Component{Manifest: manifest.Manifest{
		ID: disabledID, Name: "Disabled", Version: "1", Delivery: manifest.DeliveryInProcess,
	}, Lifecycle: lifecycle.Noop{}})
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	if err := installstate.Store(path, installstate.Metadata{Version: 1, Binary: "full", Components: []installstate.InstalledComponent{
		{ID: disabledID, Delivery: manifest.DeliveryInProcess, Installed: true},
		{ID: "test-state-missing-code", Delivery: manifest.DeliveryInProcess, Installed: true},
	}}); err != nil {
		t.Fatal(err)
	}
	components, err := Components()
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 2 {
		t.Fatalf("installed owner disappeared: %+v", components)
	}
	for _, component := range components {
		if !component.Installed || component.Enabled || component.Active || component.ObservedLifecycle.InstanceRetention != InstanceUnknown {
			t.Fatalf("eligibility became observed success: %+v", component)
		}
		if component.ID == disabledID {
			if !component.AvailableInBinary || component.ObservedLifecycle.Phase != LifecycleNotObserved {
				t.Fatalf("disabled owner misclassified: %+v", component)
			}
		} else if component.AvailableInBinary || component.Version != "" || component.ObservedLifecycle.Phase != LifecycleBinaryAbsent {
			t.Fatalf("absent code acquired a fabricated manifest: %+v", component)
		}
	}
}
