package operations

import (
	"context"
	"errors"
	"testing"
	"time"

	repository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

type failedRuntimeReconciler struct{ calls int }

func (r *failedRuntimeReconciler) Reconcile(context.Context, repository.OperationLockModel) (ReconcileDecision, error) {
	r.calls++
	return ReconcileDecision{}, &RuntimeRecoveryError{Step: "boot_restore_fence_failed", Cause: errors.New("private persistence failure")}
}

func TestRuntimeExecutionFailureKeepsRunnerAndBoundsAttemptsAcrossRestart(t *testing.T) {
	repo := operationTestRepository(t)
	owner := operationTestManager(t, repo, Options{InstanceID: "owner", PID: 201})
	acquired, err := owner.Acquire(t.Context(), acquireFixture("runtime-execution"))
	if err != nil {
		t.Fatal(err)
	}
	applying, err := owner.Transition(t.Context(), acquired.Operation.OperationID, acquired.Operation.Revision, StateApplying)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Transition(t.Context(), applying.OperationID, applying.Revision, StateApplied); err != nil {
		t.Fatal(err)
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	recovery := operationTestManager(t, repo, Options{InstanceID: "recovery", PID: 202, RecoveryEvery: time.Hour})
	backend := &failedRuntimeReconciler{}
	if err := recovery.SetReconcilerForKind(KindFirewall, backend); err != nil {
		t.Fatal(err)
	}
	if err := recovery.Start(t.Context()); err != nil {
		t.Fatalf("execution error destroyed owner: %v", err)
	}
	recovery.mu.Lock()
	started, done := recovery.started, recovery.done
	recovery.mu.Unlock()
	if !started || done == nil {
		t.Fatal("runner not started")
	}
	if _, err := recovery.Recover(t.Context()); err == nil {
		t.Fatal("execution error silently swallowed")
	}
	for i := 0; i < 3; i++ {
		if _, err := recovery.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := repo.OperationByID(t.Context(), applying.OperationID)
	if err != nil || backend.calls != 2 || stored.RecoveryAttempts != 2 || stored.RecoveryErrorCode != "runtime_restore_execution_failed" || stored.State != StateReconcileRequired {
		t.Fatalf("unbounded or unrecorded failure: calls=%d state=%s attempts=%d err=%v", backend.calls, stored.State, stored.RecoveryAttempts, err)
	}
	if err := recovery.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	restarted := operationTestManager(t, repo, Options{InstanceID: "restart", PID: 203})
	if err := restarted.SetReconcilerForKind(KindFirewall, backend); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if backend.calls != 2 {
		t.Fatal("process restart reset durable execution budget")
	}
}

type failedRuntimeFinalizeStore struct {
	Store
	failed bool
}

func (s *failedRuntimeFinalizeStore) RecoverOperationLock(ctx context.Context, update repository.RecoveryOperationLockUpdate) (repository.OperationLockModel, error) {
	if !s.failed && len(update.FromStates) == 1 && update.FromStates[0] == StateRestoringRuntime {
		s.failed = true
		return repository.OperationLockModel{}, errors.New("controlled finalization failure")
	}
	return s.Store.RecoverOperationLock(ctx, update)
}

type completedRuntimeReconciler struct {
	manager   *Manager
	mutations int
}

func (r *completedRuntimeReconciler) Reconcile(ctx context.Context, operation repository.OperationLockModel) (ReconcileDecision, error) {
	if r.mutations == 0 {
		if _, err := r.manager.Transition(ctx, operation.OperationID, operation.Revision, StateRestoringRuntime); err != nil {
			return ReconcileDecision{}, err
		}
		r.mutations++
	}
	return ReconcileDecision{State: StateApplied, Reason: "runtime_health_verified"}, nil
}
func TestRuntimeFinalizationFailureKeepsOwnerWithoutRepeatingMutation(t *testing.T) {
	repo := operationTestRepository(t)
	owner := operationTestManager(t, repo, Options{InstanceID: "original", PID: 301})
	acquired, err := owner.Acquire(t.Context(), acquireFixture("runtime-finalize"))
	if err != nil {
		t.Fatal(err)
	}
	applying, err := owner.Transition(t.Context(), acquired.Operation.OperationID, acquired.Operation.Revision, StateApplying)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Transition(t.Context(), applying.OperationID, applying.Revision, StateApplied); err != nil {
		t.Fatal(err)
	}
	if err := owner.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	store := &failedRuntimeFinalizeStore{Store: repo}
	manager := NewManager(store, Options{InstanceID: "finalizer", PID: 302, RecoveryEvery: time.Hour})
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	backend := &completedRuntimeReconciler{manager: manager}
	if err := manager.SetReconcilerForKind(KindFirewall, backend); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(t.Context()); err != nil {
		t.Fatalf("finalization failure killed owner: %v", err)
	}
	row, err := repo.OperationByID(t.Context(), applying.OperationID)
	if err != nil || row.State != StateRestoringRuntime || row.RecoveryAttempts != 1 || !store.failed {
		t.Fatalf("finalization failure not retained: %+v %v", row, err)
	}
	if _, err := manager.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	row, err = repo.OperationByID(t.Context(), applying.OperationID)
	if err != nil || row.State != StateApplied || backend.mutations != 1 {
		t.Fatalf("finalization repeated mutation: %+v %v", row, err)
	}
}
