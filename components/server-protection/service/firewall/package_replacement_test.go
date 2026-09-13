package firewall

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	artifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	helper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	operations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	repository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/MalenkiySolovey/solovey-ui/database/migration"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	sptest "github.com/MalenkiySolovey/solovey-ui/testsupport/serverprotection"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPackageReplacementProductionManagementLifecycle(t *testing.T) {
	for _, scenario := range []string{"matching", "absent", "foreign", "drifted", "composition_schema", "contribution_schema", "runtime_schema", "migration_failure", "panel_health", "subscription_health", "ssh_ipv4_health", "ssh_ipv6_health", "rolled_back"} {
		t.Run(scenario, func(t *testing.T) {
			assertCurrentSSHProductionLifecycle(t, true, "", "sqlite_package_"+scenario)
		})
	}
}

// Package script execution is separately qualified by the real APK v3 fixture.
// This layer preserves the live helper boundary and production SQLite DB across
// owner destruction/reconstruction. It reuses the existing production resource,
// SSH, baseline, workflow, health and operation composition, not a second catalog.
func assertPackageReplacement(t *testing.T, w *Workflow, nft *testHelperInvoker, db *gorm.DB, storage *artifacts.Storage, baseline *BaselineService, id, scenario string, setHealthFault func(string)) {
	t.Helper()
	ctx := t.Context()
	before, err := w.Contributions.FirewallAuthority(ctx)
	if err != nil {
		t.Fatal(err)
	}
	original, err := repository.New(db).OperationByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if scenario == "rolled_back" {
		if _, err := w.Rollback(ctx, id, "ROLLBACK SERVER PROTECTION "+id); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	var databases []struct{ Name, File string }
	if err := db.Raw("PRAGMA database_list").Scan(&databases).Error; err != nil {
		t.Fatal(err)
	}
	var path string
	for _, entry := range databases {
		if entry.Name == "main" {
			path = entry.File
		}
	}
	if path == "" {
		t.Fatal("production file-backed SQLite required")
	}
	if scenario == "migration_failure" {
		// Model an incompatible migration journal in the private database. The
		// real open-time migration gate must reject it before component startup.
		if err := migration.EnsureCurrentSchemaJournal(db, true); err != nil {
			t.Fatal(err)
		}
		changed := db.Exec("UPDATE migration_journal_v1 SET checksum=? WHERE scope='core' AND owner_id='core'", strings.Repeat("0", 64))
		if changed.Error != nil || changed.RowsAffected == 0 {
			t.Fatalf("migration fixture: %v rows=%d", changed.Error, changed.RowsAffected)
		}
	}
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if scenario == "migration_failure" {
		if err := dbsqlite.Init(path); err == nil || !strings.Contains(err.Error(), "migration journal") || dbsqlite.DB() != nil {
			t.Fatalf("failed migration published database: %v", err)
		}
		read, err := gorm.Open(sqlite.Open(path+"?mode=ro"), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		pool, err := read.DB()
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		retained, err := repository.New(read).FirewallAuthority(ctx)
		if err != nil || !reflect.DeepEqual(retained.Composition, before.Composition) || !nft.ManagedTablePresent || helperOperationCount(nft.Requests, helper.OperationNFTApply) != 1 {
			t.Fatal("migration failure minted or discarded firewall authority")
		}
		return
	}
	if err := dbsqlite.Init(path); err != nil {
		t.Fatal(err)
	}
	db = dbsqlite.DB()
	if err := repository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	baseline.Repository = repo
	previousRuntime := firewallBaselineRuntimeRevision
	firewallBaselineRuntimeRevision = hostresources.Revision("package-process-generation-B")
	t.Cleanup(func() { firewallBaselineRuntimeRevision = previousRuntime })

	switch scenario {
	case "absent":
		nft.ManagedTablePresent = false
	case "foreign":
		nft.Responses[helper.OperationNFTObserve] = helper.Response{OK: false, Code: helper.CodeValidationFailed}
	case "drifted":
		nft.ManagedCandidateSemantic = hostresources.Revision("changed-live-policy")
	case "composition_schema":
		if err := db.Model(&repository.FirewallCompositionModel{}).Where("id = 1").Update("schema", "future/v999").Error; err != nil {
			t.Fatal(err)
		}
	case "contribution_schema":
		if err := db.Model(&repository.FirewallContributionModel{}).Where("contribution_id <> ''").Update("schema", "future/v999").Error; err != nil {
			t.Fatal(err)
		}
	case "runtime_schema":
		changed := before.Composition
		changed.Runtime.Schema = "future/v999"
		if err := db.Save(&changed).Error; err != nil {
			t.Fatal(err)
		}
	}
	restarted := operations.NewManager(repo, operations.Options{InstanceID: "package-process-B", PID: 78, PIDProbe: stoppedRuntimePID{}, Audit: func(context.Context, operations.AuditEvent) error { return nil }})
	t.Cleanup(func() { _ = restarted.Stop(context.Background()) })
	newStorage, err := artifacts.NewWithRecoveryProjection(storage.Root(), sptest.SystemdRecoveryProjection())
	if err != nil {
		t.Fatal(err)
	}
	client, err := helper.NewClient(sptest.ManagedRoot(t, storage.Root()), restarted, nft, &helperAudit{})
	if err != nil {
		t.Fatal(err)
	}
	w.Manager, w.Helper, w.Contributions, w.RuntimeStore = restarted, client, repo, repo
	w.Artifacts = artifacts.Service{Storage: newStorage, Store: repo}
	w.Marker, w.State = newStorage, newStorage
	w.CurrentRuntimeManagement = baseline.RuntimeManagement
	if err := restarted.SetReconcilerForKind(operations.KindFirewall, RuntimeLossReconciler{Workflow: w}); err != nil {
		t.Fatal(err)
	}
	if err := restarted.SetRecovery(BackendRecovery{Helper: client, Manager: restarted, Storage: newStorage, Repository: repo, Health: w.RollbackHealth, Workflow: w}); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := restarted.Recover(ctx); err != nil {
			t.Fatal(err)
		}
	}
	after, err := repo.FirewallAuthority(ctx)
	if err != nil {
		t.Fatal(err)
	}
	op, err := repo.OperationByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if helperOperationCount(nft.Requests, helper.OperationNFTApply) != 1 {
		t.Fatal("package startup repeated forward mutation")
	}
	if after.HasComposition && (after.Composition.Runtime.AttemptBoot != before.Composition.Runtime.AttemptBoot || after.Composition.Runtime.MutationAt != before.Composition.Runtime.MutationAt) {
		t.Fatal("same boot was classified as restore")
	}
	if scenario == "absent" || scenario == "rolled_back" {
		if after.HasComposition || len(after.Contributions) != 0 || nft.ManagedTablePresent {
			t.Fatal("inactive authority resurrected")
		}
		want := operations.StateForgotten
		if scenario == "rolled_back" {
			want = operations.StateRolledBack
		}
		if op.State != want {
			t.Fatalf("state=%s want=%s", op.State, want)
		}
		return
	}
	if scenario == "foreign" || scenario == "drifted" || strings.HasSuffix(scenario, "schema") {
		if op.State != operations.StateReconcileRequired || !after.HasComposition || !nft.ManagedTablePresent || helperOperationCount(nft.Requests, helper.OperationNFTRollback) != 0 {
			t.Fatal("incompatible/foreign runtime was adopted or overwritten")
		}
		return
	}
	if op.State != operations.StateApplied || op.OperationID != original.OperationID || op.Revision != original.Revision || after.Observation.State != FirewallLiveMatching || !reflect.DeepEqual(before.Composition.Runtime, after.Composition.Runtime) {
		t.Fatal("matching replacement changed original lifecycle or minted a health seal")
	}
	// Historical HEALTH_VERIFIED is not current health. This call is the same
	// live owner evaluator used by status; invoke it only after reconstruction.
	started := time.Now()
	current, err := baseline.RuntimeManagement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	results := w.Health(ctx, current.Resources)
	if len(results) != 4 || healthFailedFor(current.Resources, results) {
		t.Fatalf("fresh four-resource health failed: %+v", results)
	}
	if strings.HasSuffix(scenario, "_health") {
		setHealthFault(strings.TrimSuffix(scenario, "_health"))
		results = w.Health(ctx, current.Resources)
		if !healthFailedFor(current.Resources, results) {
			t.Fatal("old successful seal hid new health failure")
		}
		return
	}
	t.Logf("new process current health: four resources PASS, collected after %s; historical health unchanged", started.UTC().Format(time.RFC3339Nano))
	rolled, err := w.Rollback(ctx, id, "ROLLBACK SERVER PROTECTION "+id)
	if err != nil || rolled.State != operations.StateRolledBack {
		t.Fatalf("same operation rollback: %v", err)
	}
	after, err = repo.FirewallAuthority(ctx)
	if err != nil || after.HasComposition || len(after.Contributions) != 0 || nft.ManagedTablePresent || helperOperationCount(nft.Requests, helper.OperationNFTRollback) != 1 {
		t.Fatal("normal rollback did not clean baseline")
	}
	if _, err := restarted.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if helperOperationCount(nft.Requests, helper.OperationNFTApply) != 1 || nft.ManagedTablePresent {
		t.Fatal("empty authority resurrected")
	}
}
