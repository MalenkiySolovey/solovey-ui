//go:build !minimal

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/migrations"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/supervisor"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	componentmanager "github.com/MalenkiySolovey/solovey-ui/components/panel-update-ui/service"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
)

const durableFallbackID = "fallback-html"

func fallbackDurableManifest(t *testing.T) manifest.Manifest {
	t.Helper()
	component, ok := registry.ComponentByID(durableFallbackID)
	if !ok || component.Durable == nil {
		t.Fatal("typed Fallback durable owner is unavailable")
	}
	return component.Manifest
}

func durableAdmissionFixture(t *testing.T, installed bool) (*supervisor.Supervisor, componentmanager.RuntimeManager, *datalifecycle.Manager) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	t.Setenv(installstate.InstalledFileEnv, filepath.Join(dir, "installed.json"))
	metadata := installstate.Metadata{Version: 1, Profile: "full", Binary: "full"}
	if installed {
		metadata.Components = []installstate.InstalledComponent{{ID: durableFallbackID, Delivery: "in-process", Installed: true}}
	}
	if err := installstate.Store(installstate.DefaultPath(), metadata); err != nil {
		t.Fatal(err)
	}
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	s := supervisor.New(componenthost.Deps{})
	data := datalifecycle.NewManager()
	data.Drop = s.DropData
	data.Admit = func(string) bool { return true } // resource-pressure policy is qualified separately
	manager := componentmanager.NewRuntimeManager(nil)
	manager.Migrate, manager.Reconcile = s.Migrate, s.Reconcile
	manager.Removal = &componentmanager.RemovalAdmission{Preview: data.PreviewRemoval, Resources: data.RemovalResources,
		Inventory: resources.Refresh, Admit: s.TryAdmitRemoval}
	return s, manager, data
}

func TestTypedDurableFreshInstallValidatesExistingSchemaAndRetainsDebt(t *testing.T) {
	s, manager, _ := durableAdmissionFixture(t, false)
	before := fallbackservice.DefaultRuntime.Status()
	status, err := manager.Install(componentmanager.OperationContext{RequestContext: t.Context()}, durableFallbackID)
	if err != nil || !status.Installed || status.Enabled {
		t.Fatalf("install: %+v %v", status, err)
	}
	db := dbsqlite.DB()
	for _, table := range fallbackDurableManifest(t).Database.Tables {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing declared table %s", table)
		}
	}
	if fallbackservice.DefaultRuntime.Status() != before {
		t.Fatal("disabled install started publication work")
	}
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	// An APPLIED journal cannot silently excuse a missing declared table.
	if err := db.Migrator().DropTable(&fallbackdomain.Event{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); err == nil {
		t.Fatal("existing schema validation skipped a missing table")
	}
	if db.Migrator().HasTable(&fallbackdomain.Event{}) {
		t.Fatal("validation silently reran migration")
	}
	step, err := migrations.StepFor(fallbackDurableManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MigrationJournal{}).Where("owner_id = ? AND step_id = ?", durableFallbackID, step.StepID).Update("checksum", strings.Repeat("f", 64)).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); err == nil {
		t.Fatal("migration checksum debt was accepted")
	}
	var journal model.MigrationJournal
	if err := db.Where("owner_id = ? AND step_id = ?", durableFallbackID, step.StepID).First(&journal).Error; err != nil {
		t.Fatal(err)
	}
	if journal.State != "RECOVERY_REQUIRED" {
		t.Fatalf("migration debt lost: %+v", journal)
	}
	owners, err := installstate.InstalledComponents()
	if err != nil || len(owners) != 1 || owners[0].ID != durableFallbackID {
		t.Fatalf("durable owner lost: %v %v", owners, err)
	}
}

func authorizedComponentRemoval(ctx context.Context) componentmanager.OperationContext {
	return componentmanager.OperationContext{RequestContext: ctx, Authorize: func(_ context.Context, invocation actions.Invocation) (actions.Decision, error) {
		return actions.Decision{Allowed: true, Actor: "fixture-admin", Permission: actions.PermissionComponentUninstall, Invocation: invocation}, nil
	}}
}

func TestTypedDurableFailedInstallAndRemovePreserveOwnership(t *testing.T) {
	t.Run("failed_install", func(t *testing.T) {
		s, manager, _ := durableAdmissionFixture(t, false)
		manager.Migrate = func(ctx context.Context) error {
			if err := s.Migrate(ctx); err != nil {
				return err
			}
			return errors.New("migration response unavailable")
		}
		if _, err := manager.Install(componentmanager.OperationContext{RequestContext: t.Context()}, durableFallbackID); err == nil {
			t.Fatal("failed install reported success")
		}
		owners, err := installstate.InstalledComponents()
		if err != nil || len(owners) != 1 || owners[0].ID != durableFallbackID {
			t.Fatal("failed install discarded durable owner", err)
		}
		for _, table := range fallbackDurableManifest(t).Database.Tables {
			if !dbsqlite.DB().Migrator().HasTable(table) {
				t.Fatal("failed install altered owner schema")
			}
		}
	})
	t.Run("failed_remove", func(t *testing.T) {
		s, manager, data := durableAdmissionFixture(t, true)
		if err := s.Migrate(t.Context()); err != nil {
			t.Fatal(err)
		}
		preview, err := data.Preview(t.Context(), durableFallbackID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := data.Execute(t.Context(), datalifecycle.ExecuteRequest{OwnerID: durableFallbackID, ExpectedPreviewRevision: preview.Revision, IdempotencyKey: "remove-clean-owner", Confirmation: "DROP_DATA_FALLBACK_HTML", BackupAcknowledged: true}); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(installstate.DefaultPath())
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		manager.Reconcile = func(ctx context.Context) error {
			calls++
			if calls == 1 {
				return errors.New("reconcile rejected")
			}
			return s.Reconcile(ctx)
		}
		if _, err := manager.Remove(authorizedComponentRemoval(t.Context()), durableFallbackID); err == nil {
			t.Fatal("failed remove reported success")
		}
		after, err := os.ReadFile(installstate.DefaultPath())
		if err != nil || string(before) != string(after) || calls != 2 {
			t.Fatalf("failed remove did not compensate exactly: %v calls=%d", err, calls)
		}
	})
}

func TestTypedDurableActiveOperationBlocksRemoval(t *testing.T) {
	s, manager, data := durableAdmissionFixture(t, true)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	row := model.DataLifecycleOperation{OperationID: "unfinished-owner-operation", IdempotencyKey: "unfinished-owner-operation", OwnerID: durableFallbackID, Kind: "DROP_DATA", State: "RECOVERY_REQUIRED", Revision: 1}
	if err := dbsqlite.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	preview, err := data.PreviewRemoval(t.Context(), durableFallbackID)
	if err != nil || len(preview.Operations) == 0 {
		t.Fatalf("unfinished operation not observed: %+v %v", preview, err)
	}
	before, err := os.ReadFile(installstate.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Remove(authorizedComponentRemoval(t.Context()), durableFallbackID); err == nil {
		t.Fatal("active/recovery operation admitted removal")
	}
	after, err := os.ReadFile(installstate.DefaultPath())
	if err != nil || string(before) != string(after) {
		t.Fatal("denied removal changed installed ownership", err)
	}
}
