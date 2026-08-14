package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestRuntimeManagerRemovePreservesOwnedDataAndReconciles(t *testing.T) {
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
	}
	status, err := manager.Remove(OperationContext{}, id)
	if err != nil {
		t.Fatal(err)
	}
	if status.ID != id || status.Installed {
		t.Fatalf("removed component status: %#v", status)
	}
	if len(events) != 1 || events[0] != "reconcile" {
		t.Fatalf("remove lifecycle events: %#v", events)
	}
	installed, err := installstate.InstalledComponents()
	if err != nil || len(installed) != 0 {
		t.Fatalf("removed component remained installed: %#v err=%v", installed, err)
	}
}

func TestRuntimeManagerRemoveRestoresInstalledStateAfterReconcileFailure(t *testing.T) {
	const id = "test-runtime-remove-compensation"
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

	reconcileCalls := 0
	manager := RuntimeManager{Reconcile: func(context.Context) error {
		reconcileCalls++
		if reconcileCalls == 1 {
			return errors.New("injected reconcile failure")
		}
		return nil
	}}
	if _, err := manager.Remove(OperationContext{}, id); err == nil {
		t.Fatal("expected remove failure")
	}
	installed, err := installstate.InstalledIDs(registeredManifests())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := installed[id]; !ok {
		t.Fatal("failed remove did not restore installed state")
	}
	if reconcileCalls != 2 {
		t.Fatalf("reconcile calls=%d, want failed attempt plus compensation", reconcileCalls)
	}
}

func TestRuntimeManagerInstallRestoresUninstalledStateAfterReconcileFailure(t *testing.T) {
	const id = "test-runtime-install-compensation"
	registerRuntimeTestComponent(id)
	installedPath := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := installstate.Store(installedPath, installstate.Metadata{Version: 1, Profile: "full", Binary: "full"}); err != nil {
		t.Fatal(err)
	}

	reconcileCalls := 0
	manager := RuntimeManager{
		Migrate: func(context.Context) error { return nil },
		Reconcile: func(context.Context) error {
			reconcileCalls++
			if reconcileCalls == 1 {
				return errors.New("injected reconcile failure")
			}
			return nil
		},
	}
	if _, err := manager.Install(OperationContext{}, id); err == nil {
		t.Fatal("expected install failure")
	}
	installed, err := installstate.InstalledIDs(registeredManifests())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := installed[id]; ok {
		t.Fatal("failed install did not restore uninstalled state")
	}
	if reconcileCalls != 2 {
		t.Fatalf("reconcile calls=%d, want failed attempt plus compensation", reconcileCalls)
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
