package service

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestRuntimeManagerRemoveWithDeleteDataDropsAfterReconcile(t *testing.T) {
	const id = "test-runtime-remove"
	registerRuntimeTestComponent(id)

	installedPath := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := installstate.Store(installedPath, installstate.Metadata{
		Version: 1,
		Profile: "full",
		Binary:  "full",
		Components: []installstate.InstalledComponent{
			{ID: id, Delivery: manifest.DeliveryInProcess, Installed: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	var events []string
	manager := RuntimeManager{
		Reconcile: func(context.Context) error {
			events = append(events, "reconcile")
			return nil
		},
		DropData: func(_ context.Context, gotID string) error {
			events = append(events, "drop:"+gotID)
			return nil
		},
	}
	status, err := manager.Remove(OperationContext{}, id, true)
	if err != nil {
		t.Fatal(err)
	}
	if status.Installed {
		t.Fatalf("removed component status = %#v", status)
	}
	if want := []string{"reconcile", "drop:" + id}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

func registerRuntimeTestComponent(id string) {
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
