package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	artifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	helper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	operations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	sptest "github.com/MalenkiySolovey/solovey-ui/testsupport/serverprotection"
	"gorm.io/gorm"
)

type expiredProcessSeed struct {
	Database, Root, OperationID, CompositionRevision string
}

func writeExpiredProcessSeed(t *testing.T, db *gorm.DB, root, operationID, revision, manifest string) {
	t.Helper()
	path := filepath.Join(filepath.Dir(manifest), "expired.db")
	// Export a consistent local test fixture. Subsequent processes open this
	// same durable database, never reseeding or changing its lifecycle rows.
	if err := db.Exec("VACUUM INTO ?", path).Error; err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(expiredProcessSeed{path, root, operationID, revision})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredRuntimeProcessRestartWindows(t *testing.T) {
	for _, window := range []string{"before_terminal", "after_terminal", "before_retirement", "after_retirement"} {
		t.Run(window, func(t *testing.T) {
			manifest := filepath.Join(t.TempDir(), "seed.json")
			assertCurrentSSHProductionLifecycle(t, true, "", "sqlite_expired_reconcile", "process_seed", manifest)
			for _, phase := range []string{"interrupt", "resume"} {
				cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestExpiredRuntimeProcessChild$", "-test.v")
				cmd.Env = append(os.Environ(), "SOLOVEY_EXPIRED_PROCESS_SEED="+manifest, "SOLOVEY_EXPIRED_PROCESS_WINDOW="+window, "SOLOVEY_EXPIRED_PROCESS_PHASE="+phase)
				output, err := cmd.CombinedOutput()
				if phase == "interrupt" {
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != 83 {
						t.Fatalf("process did not stop at exact boundary: %v", err)
					}
				} else if err != nil {
					// Startup logs may contain generated credentials. Surface only
					// explicit test assertions, never arbitrary child output.
					for _, line := range strings.Split(string(output), "\n") {
						if strings.Contains(line, "expired_process_test.go:") {
							t.Log(line)
						}
					}
					t.Fatalf("fresh process continuation failed: %v", err)
				}
				t.Logf("%s process=%s verified", window, phase)
			}
		})
	}
}

// This entry point runs in a separate OS process with no retained Go objects.
// Exit at the injected commit boundary deliberately bypasses deferred cleanup.
func TestExpiredRuntimeProcessChild(t *testing.T) {
	manifest := os.Getenv("SOLOVEY_EXPIRED_PROCESS_SEED")
	if manifest == "" {
		t.Skip("subprocess entry point")
	}
	data, err := os.ReadFile(manifest)
	var seed expiredProcessSeed
	if err != nil || json.Unmarshal(data, &seed) != nil {
		t.Fatal("cannot open process seed")
	}
	window, phase := os.Getenv("SOLOVEY_EXPIRED_PROCESS_WINDOW"), os.Getenv("SOLOVEY_EXPIRED_PROCESS_PHASE")
	workflow, executor, _, repo, _ := newWorkflowWithRoot(t, nil, workflowDatabaseConfig{Open: func(string) (*gorm.DB, error) {
		return productionStartupSQLite(t)(seed.Database)
	}})
	storage, err := artifacts.NewWithRecoveryProjection(seed.Root, sptest.SystemdRecoveryProjection())
	if err != nil {
		t.Fatal(err)
	}
	ownerClock := time.Now().UTC()
	if phase == "resume" {
		// Preserve the existing dead-PID AND expired-lease admission policy.
		// Advance only the injected owner clock, without editing the journal.
		ownerClock = ownerClock.Add(10 * time.Minute)
	}
	manager := operations.NewManager(repo, operations.Options{InstanceID: "expired-process-" + phase, PID: os.Getpid(), Now: func() time.Time { return ownerClock }, Audit: func(context.Context, operations.AuditEvent) error { return nil }})
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	client, err := helper.NewClient(sptest.ManagedRoot(t, seed.Root), manager, executor, &helperAudit{})
	if err != nil {
		t.Fatal(err)
	}
	workflow.Manager, workflow.Helper = manager, client
	workflow.Artifacts = artifacts.Service{Storage: storage, Store: repo}
	workflow.State, workflow.Marker, workflow.RuntimeStore = storage, storage, repo
	workflow.Now = func() time.Time { return time.Now().UTC() }
	// Forward capabilities are deliberately unavailable: this process owns
	// only truthful expiry and the already-authorized retirement continuation.
	assertNoMutation := func() {
		t.Helper()
		if helperOperationCount(executor.Requests, helper.OperationNFTApply) != 0 || helperOperationCount(executor.Requests, helper.OperationNFTRollback) != 0 || executor.ManagedTablePresent {
			t.Fatal("restart crossed an nft mutation boundary")
		}
	}
	interrupt := func() { assertNoMutation(); os.Exit(83) }
	if phase == "interrupt" {
		if strings.HasSuffix(window, "terminal") {
			workflow.RuntimeStore = &crashExpiredRuntime{RuntimeStore: repo, after: window == "after_terminal", interrupt: interrupt}
		} else {
			workflow.Contributions = &crashRetirementCommit{FirewallContributionStore: repo, after: window == "after_retirement", interrupt: interrupt}
		}
	} else {
		prior, err := repo.FirewallAuthority(t.Context())
		operation, opErr := repo.OperationByID(t.Context(), seed.OperationID)
		if err != nil || opErr != nil || operation.OperationID != seed.OperationID {
			t.Fatal("restart lost original operation")
		}
		if window == "after_retirement" {
			if prior.HasComposition || len(prior.Contributions) != 0 || operation.State != operations.StateRollingBack {
				t.Fatal("post-retirement crash was not captured before operation seal")
			}
		} else {
			want := "RESTORE_FAILED"
			if window == "before_terminal" {
				want = "RESTORE_REQUIRED"
			}
			if !prior.HasComposition || prior.Composition.Revision != seed.CompositionRevision || prior.Composition.Runtime.State != want || prior.Composition.Runtime.MutationAt != 0 || prior.Composition.Runtime.HealthAt != 0 || len(prior.Contributions) != 1 {
				t.Fatal("process boundary lost exact retained authority")
			}
		}
	}
	if err := manager.SetReconcilerForKind(operations.KindFirewall, RuntimeLossReconciler{Workflow: &workflow}); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetRecovery(BackendRecovery{Helper: client, Manager: manager, Storage: storage, Repository: repo, Health: workflow.RollbackHealth, Workflow: &workflow}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if phase == "interrupt" {
		_, err := workflow.Rollback(t.Context(), seed.OperationID, "ROLLBACK SERVER PROTECTION "+seed.OperationID)
		t.Fatalf("interruption boundary not reached: %v", err)
	}
	operation, err := repo.OperationByID(t.Context(), seed.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(window, "terminal") {
		if operation.State != operations.StateReconcileRequired {
			t.Fatal("terminalization did not preserve operator decision")
		}
		stable := operation.Revision
		for range 3 {
			if _, err := manager.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		operation, err = repo.OperationByID(t.Context(), seed.OperationID)
		if err != nil || operation.Revision != stable {
			t.Fatal("new process resumed revision churn")
		}
		retained, err := repo.FirewallAuthority(t.Context())
		if err != nil || !retained.HasComposition || retained.Composition.Runtime.State != "RESTORE_FAILED" || retained.Composition.Runtime.Reason != "boot_restore_deadline_expired" {
			t.Fatal("new process did not terminalize expiry")
		}
		if _, err := workflow.Rollback(t.Context(), seed.OperationID, "ROLLBACK SERVER PROTECTION "+seed.OperationID); err != nil {
			t.Fatal(err)
		}
	}
	final, err := repo.FirewallAuthority(t.Context())
	operation, opErr := repo.OperationByID(t.Context(), seed.OperationID)
	if err != nil || opErr != nil || final.HasComposition || len(final.Contributions) != 0 || operation.State != operations.StateRolledBack {
		t.Fatalf("new process failed to complete original retirement: state=%s composition=%v contributions=%d", operation.State, final.HasComposition, len(final.Contributions))
	}
	assertNoMutation()
}
