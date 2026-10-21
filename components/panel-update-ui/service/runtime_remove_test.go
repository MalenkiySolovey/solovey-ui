package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/supervisor"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRuntimeManagerRemoveCleanDisabledOwnerWithoutDroppingData(t *testing.T) {
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
	admission, _ := runtimeRemovalFixture(t)
	panel := resources.ProtectableResource{ID: "core:panel:web", Owner: "core", Source: "settings",
		Capabilities: resources.ProtectableResourceCapabilities{ConfigRevision: resources.Revision("configured panel")}}
	panel.Identity = resources.LocalConfiguredIdentity(panel)
	admission.Inventory = func(context.Context) resources.ResourceSnapshot {
		return resources.ResourceSnapshot{Resources: []resources.ProtectableResource{panel}}
	}
	manager := RuntimeManager{
		Removal: admission,
		Reconcile: func(context.Context) error {
			events = append(events, "reconcile")
			return nil
		},
	}
	status, err := manager.Remove(authorizedRemovalContext(context.Background()), id)
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
	admission, _ := runtimeRemovalFixture(t)
	manager := RuntimeManager{Removal: admission, Reconcile: func(context.Context) error {
		reconcileCalls++
		if reconcileCalls == 1 {
			return errors.New("injected reconcile failure")
		}
		return nil
	}}
	if _, err := manager.Remove(authorizedRemovalContext(context.Background()), id); err == nil {
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
	admission, _ := runtimeRemovalFixture(t)
	manager := RuntimeManager{
		Removal: admission,
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
			DefaultEnabled: false,
		},
		Lifecycle: lifecycle.Noop{},
	})
}

func runtimeRemovalFixture(t *testing.T) (*RemovalAdmission, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "removal.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Setting{}, &model.MigrationJournal{}, &model.DataLifecycleOperation{}); err != nil {
		t.Fatal(err)
	}
	data := &datalifecycle.Manager{DB: func() *gorm.DB { return db }, Now: time.Now,
		Enabled: func(item manifest.Manifest) (bool, error) { return item.DefaultEnabled, nil },
		Drop:    func(context.Context, string) error { t.Error("Remove invoked DropData"); return nil }}
	s := supervisor.New(componenthost.Deps{})
	inventory := resources.NewRegistry(time.Second)
	return &RemovalAdmission{Preview: data.PreviewRemoval, Resources: data.RemovalResources,
		Inventory: inventory.Refresh, Admit: s.TryAdmitRemoval}, db
}
