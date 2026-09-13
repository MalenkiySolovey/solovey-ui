package firewall

import (
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	helper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	operations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	repository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"gorm.io/gorm"
)

func TestRetainedRollbackAdmissionRejectsUnsafeAuthority(t *testing.T) {
	assertCurrentSSHProductionLifecycle(t, true, "", "sqlite_expired_reconcile", "operator_negatives")
}

func assertRetainedRollbackNegatives(t *testing.T, workflow Workflow, repo *repository.Repository, db *gorm.DB, executor *testHelperInvoker, operation repository.OperationLockModel) {
	t.Helper()
	before, err := repo.FirewallAuthority(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*repository.FirewallCompositionModel){
		"wrong operation":    func(c *repository.FirewallCompositionModel) { c.AppliedOperationID = "other-operation" },
		"unretired runtime":  func(c *repository.FirewallCompositionModel) { c.Runtime.State = "RESTORE_REQUIRED" },
		"other failure":      func(c *repository.FirewallCompositionModel) { c.Runtime.Reason = "boot_restore_interrupted_absent" },
		"mutation ambiguity": func(c *repository.FirewallCompositionModel) { c.Runtime.MutationAt = 1 },
		"unexpected health":  func(c *repository.FirewallCompositionModel) { c.Runtime.HealthAt = 1 },
		"invalid digest": func(c *repository.FirewallCompositionModel) {
			c.CandidateSemanticSHA256 = hostresources.Revision("wrong")
		},
		"nonactive composition": func(c *repository.FirewallCompositionModel) { c.State = "RECOVERY_REQUIRED" },
		"nonexpired deadline": func(c *repository.FirewallCompositionModel) {
			c.Runtime.StartedAt = workflow.now().Unix()
			c.Runtime.Deadline = c.Runtime.StartedAt + 300
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := before.Composition
			mutate(&changed)
			if err := db.Save(&changed).Error; err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := db.Save(&before.Composition).Error; err != nil {
					t.Error(err)
				}
			}()
			if workflow.CanResolveByRollback(t.Context(), operation) {
				t.Fatal("unsafe action advertised")
			}
			if _, err := workflow.Rollback(t.Context(), operation.OperationID, "ROLLBACK SERVER PROTECTION "+operation.OperationID); err == nil {
				t.Fatal("unsafe retirement admitted")
			}
			stored, err := repo.OperationByID(t.Context(), operation.OperationID)
			if err != nil || stored.Revision != operation.Revision || stored.State != operation.State {
				t.Fatal("rejected admission changed original fence")
			}
		})
	}
	t.Run("foreign live table", func(t *testing.T) {
		executor.ManagedTablePresent, executor.ManagedPlanRevision, executor.ManagedCandidateSemantic, executor.ManagedTimedMembership = true, hostresources.Revision("foreign"), hostresources.Revision("foreign"), hostresources.Revision("foreign")
		defer func() {
			executor.ManagedTablePresent, executor.ManagedPlanRevision, executor.ManagedCandidateSemantic, executor.ManagedTimedMembership = false, "", "", ""
		}()
		if _, err := workflow.Rollback(t.Context(), operation.OperationID, "ROLLBACK SERVER PROTECTION "+operation.OperationID); err == nil {
			t.Fatal("foreign table authorized retirement")
		}
	})
	t.Run("concurrent durable mutation", func(t *testing.T) {
		other := repository.OperationLockModel{OperationID: "concurrent-mutation", IdempotencyKey: "concurrent-mutation", State: operations.StateApplying, Kind: operations.KindFirewall, Revision: 1}
		if err := db.Create(&other).Error; err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := db.Delete(&other).Error; err != nil {
				t.Error(err)
			}
		}()
		if _, err := workflow.Rollback(t.Context(), operation.OperationID, "ROLLBACK SERVER PROTECTION "+operation.OperationID); err == nil {
			t.Fatal("concurrent mutation was ignored")
		}
		stored, err := repo.OperationByID(t.Context(), operation.OperationID)
		if err != nil || stored.Revision != operation.Revision {
			t.Fatal("failed reclaim changed durable fence")
		}
	})
	if helperOperationCount(executor.Requests, helper.OperationNFTApply) != 1 || helperOperationCount(executor.Requests, helper.OperationNFTRollback) != 0 {
		t.Fatal("negative admission mutated nft")
	}
}

func assertFreshLifecycleAfterRetirement(t *testing.T, workflow Workflow, baseline *BaselineService, executor *testHelperInvoker, repo *repository.Repository, boot *controlledBootGeneration, operator lifecycleOperator) {
	t.Helper()
	current, err := baseline.Snapshot(t.Context(), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	preview := Preview(current.Plan, PreviewOptions{IncludeGeneratedNFT: true})
	if preview.Revision != current.Plan.Revision {
		t.Fatal("fresh Preview lost current plan")
	}
	prepared, err := workflow.Prepare(t.Context(), PrepareInput{Plan: current.Plan, Actor: "ci", IdempotencyKey: "fresh-after-retirement", Confirmation: "PREPARE SERVER PROTECTION " + current.Plan.Revision})
	if err != nil {
		t.Fatalf("fresh normal Prepare after resolution: %v", err)
	}
	id := prepared.Operation.OperationID
	applied, err := workflow.Apply(t.Context(), ApplyInput{OperationID: id, Plan: current.Plan, Confirmation: "APPLY SERVER PROTECTION " + id})
	if err != nil || applied.State != operations.StateApplied {
		t.Fatalf("fresh normal Apply after resolution: %v", err)
	}
	if err := workflow.Manager.SetReconcilerForKind(operations.KindFirewall, RuntimeLossReconciler{Workflow: &workflow}); err != nil {
		t.Fatal(err)
	}
	boot.generation = hostresources.Revision("fresh-boot-after-retirement")
	executor.ManagedTablePresent, executor.ManagedPlanRevision, executor.ManagedCandidateSHA, executor.ManagedCandidateSemantic, executor.ManagedTimedMembership = false, "", "", "", ""
	if _, err := workflow.Manager.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	sealed, err := repo.FirewallAuthority(t.Context())
	if err != nil || !sealed.HasComposition || sealed.Composition.AppliedOperationID != id || sealed.Composition.Runtime.State != "HEALTH_VERIFIED" || sealed.Composition.Runtime.VerifiedBoot != boot.generation || sealed.Composition.Runtime.HealthAt <= sealed.Composition.Runtime.MutationAt {
		t.Fatal("fresh boot restore did not seal original operation with fresh health")
	}
	for range 3 {
		if _, err := workflow.Manager.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if helperOperationCount(executor.Requests, helper.OperationNFTApply) != 3 {
		t.Fatal("new lifecycle repeated or missed a mutation")
	}
	var rolled Result
	if operator != nil {
		rolled, err = operator(workflow, repo, id)
	} else {
		rolled, err = workflow.Rollback(t.Context(), id, "ROLLBACK SERVER PROTECTION "+id)
	}
	if err != nil || rolled.State != operations.StateRolledBack {
		t.Fatalf("fresh lifecycle normal Rollback: %v", err)
	}
	clean, err := repo.FirewallAuthority(t.Context())
	if err != nil || clean.HasComposition || len(clean.Contributions) != 0 || executor.ManagedTablePresent || helperOperationCount(executor.Requests, helper.OperationNFTRollback) != 1 {
		t.Fatal("fresh lifecycle did not return to clean baseline")
	}
}
