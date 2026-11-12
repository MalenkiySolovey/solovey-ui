//go:build !minimal

package fallbackhtml

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/durable"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/migrations"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/supervisor"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	fallbacklifecycle "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/lifecycle"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	componentmanager "github.com/MalenkiySolovey/solovey-ui/components/panel-update-ui/service"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"
	"github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func admissionFixture(t *testing.T, installed bool) (*supervisor.Supervisor, componentmanager.RuntimeManager, *datalifecycle.Manager) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	t.Setenv(installstate.InstalledFileEnv, filepath.Join(dir, "installed.json"))
	metadata := installstate.Metadata{Version: 1, Profile: "full", Binary: "full"}
	if installed {
		metadata.Components = []installstate.InstalledComponent{{ID: id, Delivery: "in-process", Installed: true}}
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
	s, manager, _ := admissionFixture(t, false)
	before := fallbackservice.DefaultRuntime.Status()
	status, err := manager.Install(componentmanager.OperationContext{RequestContext: t.Context()}, id)
	if err != nil || !status.Installed || status.Enabled {
		t.Fatalf("install: %+v %v", status, err)
	}
	db := dbsqlite.DB()
	for _, table := range componentManifest.Database.Tables {
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
	step, err := migrations.StepFor(componentManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MigrationJournal{}).Where("owner_id = ? AND step_id = ?", id, step.StepID).Update("checksum", strings.Repeat("f", 64)).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(t.Context()); err == nil {
		t.Fatal("migration checksum debt was accepted")
	}
	var journal model.MigrationJournal
	if err := db.Where("owner_id = ? AND step_id = ?", id, step.StepID).First(&journal).Error; err != nil {
		t.Fatal(err)
	}
	if journal.State != "RECOVERY_REQUIRED" {
		t.Fatalf("migration debt lost: %+v", journal)
	}
	owners, err := installstate.InstalledComponents()
	if err != nil || len(owners) != 1 || owners[0].ID != id {
		t.Fatalf("durable owner lost: %v %v", owners, err)
	}
}

func TestTypedDurableDropUsesActualDataLifecycleBackupAndReplay(t *testing.T) {
	s, _, data := admissionFixture(t, true)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()
	if err := db.Create(&fallbackdomain.Site{Name: "Preserved until explicit drop"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InboundDraft{Source: "other-component", SourceRef: "keep", Status: "blocked", Payload: []byte("{}")}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DropData(t.Context(), id); err == nil {
		t.Fatal("unadmitted cleanup succeeded")
	}
	preview, err := data.Preview(t.Context(), id)
	if err != nil || len(preview.Blockers) != 0 {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	request := datalifecycle.ExecuteRequest{OwnerID: id, ExpectedPreviewRevision: preview.Revision, IdempotencyKey: "typed-fallback-drop", Confirmation: "DROP_DATA_FALLBACK_HTML", BackupAcknowledged: true}
	operation, err := data.Execute(t.Context(), request)
	if err != nil || operation.State != "APPLIED" || len(operation.BackupRef) != 64 {
		t.Fatalf("actual drop: %+v %v", operation, err)
	}
	for _, table := range componentManifest.Database.Tables {
		if db.Migrator().HasTable(table) {
			t.Fatalf("owned table survived: %s", table)
		}
	}
	var foreign int64
	if err := db.Model(&model.InboundDraft{}).Where("source = ?", "other-component").Count(&foreign).Error; err != nil || foreign != 1 {
		t.Fatal("foreign draft was affected", err)
	}
	replayed, err := data.Execute(t.Context(), request)
	if err != nil || replayed.OperationID != operation.OperationID || replayed.State != "APPLIED" {
		t.Fatalf("replay: %+v %v", replayed, err)
	}
}

func TestTypedDurableCandidateRejectsLiveAndPreparationFailurePreservesLive(t *testing.T) {
	s, _, _ := admissionFixture(t, true)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()
	if err := db.Create(&fallbackdomain.Site{Name: "Live before image"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := durableBindingForTest(t).RunCandidate(t.Context(), db.Session(&gorm.Session{NewDB: true})); err == nil {
		t.Fatal("live session clone admitted as candidate")
	}
	// Distinct pools and alternate file names must not disguise live storage.
	for _, filename := range []string{configstorage.GetDBPath(), filepath.Join(t.TempDir(), "live-alias.db")} {
		if filename != configstorage.GetDBPath() {
			if err := os.Link(configstorage.GetDBPath(), filename); err != nil {
				t.Fatal(err)
			}
		}
		other, err := gorm.Open(sqlite.Open(filename), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		pool, _ := other.DB()
		if err := durableBindingForTest(t).RunCandidate(t.Context(), other); err == nil {
			t.Error("second pool to live file admitted as candidate")
		}
		_ = pool.Close()
	}
	candidate, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "candidate.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := candidate.DB()
	t.Cleanup(func() { _ = pool.Close() })
	rejection := errors.New("candidate preparation rejected")
	binding, err := durableowner.Bind(componentManifest, fallbacklifecycle.Owner{}, durableowner.BackendAdapter{
		Migrate: func(ctx context.Context, db *gorm.DB) error {
			if err := backendMigrate(ctx, db); err != nil {
				return err
			}
			return rejection
		},
		Restore: backendRestore, Inspect: backendInspect, Drop: backendDrop,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.RunCandidate(t.Context(), candidate); !errors.Is(err, rejection) {
		t.Fatalf("candidate failure: %v", err)
	}
	var site fallbackdomain.Site
	if err := db.First(&site).Error; err != nil || site.Name != "Live before image" {
		t.Fatal("candidate failure changed live state", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := durableBindingForTest(t).Migrate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled migration: %v", err)
	}
}

func TestTypedDurableDropRejectsStaleRevisionAndRetiredGeneration(t *testing.T) {
	s, _, _ := admissionFixture(t, true)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()
	row := model.DataLifecycleOperation{OperationID: "stale-drop", IdempotencyKey: "stale-drop", OwnerID: id, Kind: "DROP_DATA", State: "DROPPING", Revision: 3, BackupRef: strings.Repeat("a", 64)}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ctx, err := durableowner.ContextForDrop(t.Context(), db, id, row.OperationID, row.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&row).Update("revision", 4).Error; err != nil {
		t.Fatal(err)
	}
	err = durableBindingForTest(t).DropData(ctx)
	var failed *durable.Failure
	if !errors.As(err, &failed) || failed.Outcome != durable.Rejected {
		t.Fatalf("stale drop: %v", err)
	}
	for _, table := range componentManifest.Database.Tables {
		if !db.Migrator().HasTable(table) {
			t.Fatal("stale cleanup deleted data")
		}
	}
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatal(err)
	}
	if err := durableBindingForTest(t).DropData(ctx); err == nil {
		t.Fatal("retired storage admission was reused")
	}
}

func TestTypedDurableMaintenanceDoesNotPromoteLiveStorage(t *testing.T) {
	s, _, _ := admissionFixture(t, true)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()
	maintenance, err := dbsqlite.BeginMaintenance(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer maintenance.End()
	if err := durableBindingForTest(t).RunCandidate(maintenance.Context(t.Context()), db); err == nil {
		t.Fatal("maintenance admission confused old live storage with private candidate")
	}
}

func TestTypedDurableUnknownDropRetainsRecoveryAndInstalledOwner(t *testing.T) {
	s, _, data := admissionFixture(t, true)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	ambiguous := errors.New("cleanup outcome requires recovery")
	binding, err := durableowner.Bind(componentManifest, fallbacklifecycle.Owner{}, durableowner.BackendAdapter{
		Migrate: backendMigrate, Restore: backendRestore, Inspect: backendInspect,
		Drop: func(ctx context.Context, db *gorm.DB) error {
			if err := backendDrop(ctx, db); err != nil {
				return err
			}
			return ambiguous
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	data.Drop = func(ctx context.Context, _ string) error { return binding.DropData(ctx) }
	preview, err := data.Preview(t.Context(), id)
	if err != nil || len(preview.Blockers) != 0 {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	operation, err := data.Execute(t.Context(), datalifecycle.ExecuteRequest{OwnerID: id, ExpectedPreviewRevision: preview.Revision,
		IdempotencyKey: "ambiguous-cleanup", Confirmation: "DROP_DATA_FALLBACK_HTML", BackupAcknowledged: true})
	var failed *durable.Failure
	if !errors.Is(err, ambiguous) || !errors.As(err, &failed) || failed.Outcome != durable.Unknown || operation.State != "RECOVERY_REQUIRED" || len(operation.BackupRef) != 64 {
		t.Fatalf("ambiguous outcome was not retained: %+v %v", operation, err)
	}
	owners, err := installstate.InstalledComponents()
	if err != nil || len(owners) != 1 || owners[0].ID != id {
		t.Fatal("unknown cleanup forgot installed owner", err)
	}
	recovery, err := data.Recovery(t.Context())
	if err != nil || recovery.OperationID != operation.OperationID {
		t.Fatalf("recovery unavailable: %+v %v", recovery, err)
	}
}

func authorizedComponentRemoval(ctx context.Context) componentmanager.OperationContext {
	return componentmanager.OperationContext{RequestContext: ctx, Authorize: func(_ context.Context, invocation actions.Invocation) (actions.Decision, error) {
		return actions.Decision{Allowed: true, Actor: "fixture-admin", Permission: actions.PermissionComponentUninstall, Invocation: invocation}, nil
	}}
}

func TestTypedDurableFailedInstallAndRemovePreserveOwnership(t *testing.T) {
	t.Run("failed_install", func(t *testing.T) {
		s, manager, _ := admissionFixture(t, false)
		manager.Migrate = func(ctx context.Context) error {
			if err := s.Migrate(ctx); err != nil {
				return err
			}
			return errors.New("migration response unavailable")
		}
		if _, err := manager.Install(componentmanager.OperationContext{RequestContext: t.Context()}, id); err == nil {
			t.Fatal("failed install reported success")
		}
		owners, err := installstate.InstalledComponents()
		if err != nil || len(owners) != 1 || owners[0].ID != id {
			t.Fatal("failed install discarded durable owner", err)
		}
		for _, table := range componentManifest.Database.Tables {
			if !dbsqlite.DB().Migrator().HasTable(table) {
				t.Fatal("failed install altered owner schema")
			}
		}
	})
	t.Run("failed_remove", func(t *testing.T) {
		s, manager, data := admissionFixture(t, true)
		if err := s.Migrate(t.Context()); err != nil {
			t.Fatal(err)
		}
		preview, err := data.Preview(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := data.Execute(t.Context(), datalifecycle.ExecuteRequest{OwnerID: id, ExpectedPreviewRevision: preview.Revision, IdempotencyKey: "remove-clean-owner", Confirmation: "DROP_DATA_FALLBACK_HTML", BackupAcknowledged: true}); err != nil {
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
		if _, err := manager.Remove(authorizedComponentRemoval(t.Context()), id); err == nil {
			t.Fatal("failed remove reported success")
		}
		after, err := os.ReadFile(installstate.DefaultPath())
		if err != nil || string(before) != string(after) || calls != 2 {
			t.Fatalf("failed remove did not compensate exactly: %v calls=%d", err, calls)
		}
	})
}

func TestTypedDurableActiveOperationBlocksRemoval(t *testing.T) {
	s, manager, data := admissionFixture(t, true)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	row := model.DataLifecycleOperation{OperationID: "unfinished-owner-operation", IdempotencyKey: "unfinished-owner-operation", OwnerID: id, Kind: "DROP_DATA", State: "RECOVERY_REQUIRED", Revision: 1}
	if err := dbsqlite.DB().Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	preview, err := data.PreviewRemoval(t.Context(), id)
	if err != nil || len(preview.Operations) == 0 {
		t.Fatalf("unfinished operation not observed: %+v %v", preview, err)
	}
	before, err := os.ReadFile(installstate.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Remove(authorizedComponentRemoval(t.Context()), id); err == nil {
		t.Fatal("active/recovery operation admitted removal")
	}
	after, err := os.ReadFile(installstate.DefaultPath())
	if err != nil || string(before) != string(after) {
		t.Fatal("denied removal changed installed ownership", err)
	}
}

// The optional fixture is synthetic, produced by the new adapter and consumed
// by the actual previous source in isolated rollback qualification.
func TestTypedDurableProducesCompatibleOwnerBackup(t *testing.T) {
	s, _, _ := admissionFixture(t, true)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	setComponentTestSetting(t, "webPath", "/private-panel/")
	service := fallbackservice.New(dbsqlite.DB(), fallbackservice.NewRuntime())
	site, err := service.SaveSite(fallbackservice.SiteInput{Name: "Adapter rollback fixture"}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PublishSite(site.ID, "fixture"); err != nil {
		t.Fatal(err)
	}
	data, err := dbbackup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	if output := os.Getenv("SOLOVEY_DURABLE_COMPAT_FIXTURE"); output != "" {
		if err := os.WriteFile(output, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTypedDurableCanceledCleanupRetainsRecoveryBackup(t *testing.T) {
	s, manager, data := admissionFixture(t, true)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	binding, err := durableowner.Bind(componentManifest, fallbacklifecycle.Owner{}, durableowner.BackendAdapter{
		Migrate: backendMigrate, Restore: backendRestore, Inspect: backendInspect,
		Drop: func(ctx context.Context, db *gorm.DB) error {
			if err := backendDrop(ctx, db); err != nil {
				return err
			}
			cancel() // cleanup completed, but its caller can no longer verify it
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	data.Drop = func(ctx context.Context, _ string) error { return binding.DropData(ctx) }
	preview, err := data.Preview(ctx, id)
	if err != nil || len(preview.Blockers) != 0 {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	operation, err := data.Execute(ctx, datalifecycle.ExecuteRequest{OwnerID: id, ExpectedPreviewRevision: preview.Revision,
		IdempotencyKey: "canceled-cleanup", Confirmation: "DROP_DATA_FALLBACK_HTML", BackupAcknowledged: true})
	if !errors.Is(err, context.Canceled) || operation.State == "APPLIED" || len(operation.BackupRef) != 64 {
		t.Fatalf("canceled cleanup lost evidence: %+v %v", operation, err)
	}
	// The existing recovery owner resolves the interrupted critical phase.
	if err := data.ReconcileStartup(t.Context()); err != nil {
		t.Fatal(err)
	}
	recovery, err := data.Recovery(t.Context())
	if err != nil || recovery.State != "RECOVERY_REQUIRED" || recovery.OperationID != operation.OperationID || recovery.BackupRef != operation.BackupRef {
		t.Fatalf("canceled cleanup recovery: %+v %v", recovery, err)
	}
	if _, err := manager.Remove(authorizedComponentRemoval(t.Context()), id); err == nil {
		t.Fatal("interrupted cleanup admitted removal")
	}
}
