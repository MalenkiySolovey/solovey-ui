package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"
	"github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
	"gorm.io/gorm"
)

type removalDataOwner struct{ lifecycle.Noop }

type removalFailedStartOwner struct{ lifecycle.Noop }

func (removalFailedStartOwner) Start(context.Context, lifecycle.Context) error {
	return errors.New("injected Start failure")
}

func (removalDataOwner) DropData(context.Context, lifecycle.Context) error { return nil }

func (removalDataOwner) InspectDropAuthority(context.Context, *gorm.DB, time.Time) lifecycle.DropAuthorityStatus {
	return lifecycle.DropAuthorityStatus{State: "VERIFIED_SAFE"}
}

func installedRemovalFixture(t *testing.T, id string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	if err := installstate.Store(path, installstate.Metadata{Version: 1, Profile: "full", Binary: "full",
		Components: []installstate.InstalledComponent{{ID: id, Delivery: manifest.DeliveryInProcess, Installed: true}}}); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRemovalDenialsPreserveExactInstalledOwnership(t *testing.T) {
	const id = "test-removal-denials"
	registerRuntimeTestComponent(id)
	path := installedRemovalFixture(t, id)
	for _, reason := range []string{"active authority", "recovery debt", "unknown authority", "panic", "inventory unavailable", "external reference", "owned Local resource", "final footprint changed", "missing port", "canceled"} {
		t.Run(reason, func(t *testing.T) {
			admission, _ := runtimeRemovalFixture(t)
			original := admission.Preview
			switch reason {
			case "active authority", "recovery debt", "unknown authority":
				admission.Preview = func(ctx context.Context, owner string) (datalifecycle.Preview, error) {
					p, err := original(ctx, owner)
					p.ExternalAuthority = "BLOCKED"
					if reason == "unknown authority" {
						p.ExternalAuthority = "UNKNOWN"
					}
					p.Blockers = append(p.Blockers, reason)
					return p, err
				}
			case "panic":
				admission.Preview = func(context.Context, string) (datalifecycle.Preview, error) { panic("private-owner-secret") }
			case "inventory unavailable":
				admission.Inventory = func(context.Context) resources.ResourceSnapshot {
					return resources.ResourceSnapshot{Errors: []resources.ResourceError{{Owner: "foreign", Message: "private"}}}
				}
			case "external reference":
				admission.Inventory = func(context.Context) resources.ResourceSnapshot {
					return resources.ResourceSnapshot{Resources: []resources.ProtectableResource{{ID: "foreign:socket", Owner: "foreign", Source: "settings", Capabilities: resources.ProtectableResourceCapabilities{FallbackTargetID: id + ":target"}}}}
				}
			case "owned Local resource":
				owned := resources.ProtectableResource{ID: id + ":listener", Owner: id, Source: "settings",
					Capabilities: resources.ProtectableResourceCapabilities{ConfigRevision: resources.Revision("configured listener")}}
				owned.Identity = resources.LocalConfiguredIdentity(owned)
				admission.Inventory = func(context.Context) resources.ResourceSnapshot {
					return resources.ResourceSnapshot{Resources: []resources.ProtectableResource{owned}}
				}
			case "final footprint changed":
				final := admission.Resources
				admission.Resources = func(ctx context.Context, owner string) (datalifecycle.Preview, error) {
					p, err := final(ctx, owner)
					p.Resources = append(p.Resources, datalifecycle.Resource{Owner: id, ID: "table:new", Terminal: "PRESENT"})
					return p, err
				}
			case "missing port":
				admission.Admit = nil
			}
			ctx := t.Context()
			if reason == "canceled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			reconciled := false
			manager := RuntimeManager{Removal: admission, Reconcile: func(context.Context) error { reconciled = true; return nil }}
			_, err = manager.Remove(OperationContext{RequestContext: ctx}, id)
			after, readErr := os.ReadFile(path)
			if err == nil || readErr != nil || !bytes.Equal(before, after) || reconciled || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe removal: error=%v changed=%v reconcile=%v read=%v", err, !bytes.Equal(before, after), reconciled, readErr)
			}
		})
	}
}

func TestRemovalRetainsDeclaredSchemaAndMigrationDebt(t *testing.T) {
	const id = "test-removal-durable"
	item := manifest.Manifest{ID: id, Name: id, Version: "1", Delivery: manifest.DeliveryInProcess,
		Database: manifest.Database{Tables: []string{"removal_owner_data"}}}
	registry.Register(registry.Component{Manifest: item, Lifecycle: removalDataOwner{}})
	path := installedRemovalFixture(t, id)
	admission, db := runtimeRemovalFixture(t)
	if err := db.Exec("CREATE TABLE removal_owner_data (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	manager := RuntimeManager{Removal: admission}
	preview, err := admission.Preview(t.Context(), id)
	if err != nil || len(preview.Blockers) != 0 {
		t.Fatalf("fixture cannot reach data admission: %+v %v", preview, err)
	}
	before, _ := os.ReadFile(path)
	if _, err := manager.Remove(OperationContext{}, id); err == nil {
		t.Fatal("empty declared schema was orphaned")
	}
	after, _ := os.ReadFile(path)
	owners, err := installstate.InstalledComponents()
	ownerManifest, available := durableowner.Lookup(id)
	if err != nil || len(owners) != 1 || owners[0].ID != id || !available || len(ownerManifest.Database.Tables) != 1 || !bytes.Equal(before, after) {
		t.Fatalf("backup owner inventory lost: %v %v %v", owners, available, err)
	}
	if err := db.Migrator().DropTable("removal_owner_data"); err != nil {
		t.Fatal(err)
	}
	preview, err = admission.Preview(t.Context(), id)
	if err != nil || validateRemovalResources(id, preview, true, true) != nil {
		t.Fatalf("clean data footprint did not become admissible: %+v %v", preview, err)
	}
	journal := model.MigrationJournal{Scope: "component", OwnerID: id, StepID: "schema:1", Checksum: "test", State: "FAILED"}
	if err := db.Create(&journal).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Remove(OperationContext{}, id); err == nil {
		t.Fatal("failed migration lost its installed owner")
	}
}

func TestFailedInstallRetainsPartialMigrationOwner(t *testing.T) {
	const id = "test-install-partial-owner"
	registerRuntimeTestComponent(id)
	path := installedRemovalFixture(t, id)
	if err := installstate.Store(path, installstate.Metadata{Version: 1, Profile: "full", Binary: "full"}); err != nil {
		t.Fatal(err)
	}
	admission, db := runtimeRemovalFixture(t)
	manager := RuntimeManager{Removal: admission, Migrate: func(context.Context) error {
		if err := db.Create(&model.MigrationJournal{Scope: "component", OwnerID: id, StepID: "partial", Checksum: "test", State: "FAILED"}).Error; err != nil {
			return err
		}
		return errors.New("injected migration failure")
	}, Reconcile: func(context.Context) error { t.Error("unsafe install compensation reconciled"); return nil }}
	if _, err := manager.Install(OperationContext{}, id); err == nil {
		t.Fatal("expected migration failure")
	}
	owners, err := installstate.InstalledComponents()
	if err != nil || len(owners) != 1 || owners[0].ID != id {
		t.Fatalf("partial migration owner forgotten: %v %v", owners, err)
	}
}

func TestFailedInstallRetainsUnknownStartInstance(t *testing.T) {
	const id = "test-install-start-failure"
	registry.Register(registry.Component{Manifest: manifest.Manifest{ID: id, Name: id, Version: "1", Delivery: manifest.DeliveryInProcess, DefaultEnabled: true}, Lifecycle: removalFailedStartOwner{}})
	path := installedRemovalFixture(t, id)
	if err := installstate.Store(path, installstate.Metadata{Version: 1, Profile: "full", Binary: "full"}); err != nil {
		t.Fatal(err)
	}
	admission, _ := runtimeRemovalFixture(t)
	s := supervisor.New(componenthost.Deps{})
	admission.Admit = s.TryAdmitRemoval
	manager := RuntimeManager{Removal: admission, Migrate: func(context.Context) error { return nil }, Reconcile: s.Reconcile}
	if _, err := manager.Install(OperationContext{}, id); err == nil {
		t.Fatal("expected Start failure")
	}
	owners, err := installstate.InstalledComponents()
	if err != nil || len(owners) != 1 || owners[0].ID != id {
		t.Fatalf("unknown Start instance lost ownership: %+v %v", owners, err)
	}
}

func TestRemovalTimeoutDoesNotAdmitLateMutationOrUnboundedWorkers(t *testing.T) {
	const id = "test-removal-timeout"
	registerRuntimeTestComponent(id)
	path := installedRemovalFixture(t, id)
	admission, _ := runtimeRemovalFixture(t)
	original := admission.Preview
	release, returned := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	admission.Preview = func(ctx context.Context, owner string) (datalifecycle.Preview, error) {
		calls.Add(1)
		defer close(returned)
		<-release
		return original(ctx, owner)
	}
	before, _ := os.ReadFile(path)
	manager := RuntimeManager{Removal: admission, Timeout: 10 * time.Millisecond}
	if _, err := manager.Remove(OperationContext{}, id); err == nil {
		t.Fatal("timeout admitted removal")
	}
	for range 5 {
		if _, err := manager.Remove(OperationContext{}, id); err == nil {
			t.Fatal("busy observation admitted removal")
		}
	}
	close(release)
	<-returned
	deadline := time.Now().Add(time.Second)
	for len(removalReads) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	after, _ := os.ReadFile(path)
	if calls.Load() != 1 || len(removalReads) != 0 || !bytes.Equal(before, after) {
		t.Fatalf("late mutation/work growth: calls=%d", calls.Load())
	}
}
