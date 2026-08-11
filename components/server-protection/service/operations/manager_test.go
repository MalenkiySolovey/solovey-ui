package operations

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakePIDProbe struct {
	alive bool
	err   error
}

func (p fakePIDProbe) Alive(int) (bool, error) { return p.alive, p.err }

type fakeRecovery struct {
	hasArtifact bool
	evidenceErr error
	rollbackErr error
	rollbacks   int
	bundles     int
}

type fakeReconciler struct {
	decision      ReconcileDecision
	calls         int
	seen          []protectionrepository.OperationLockModel
	validate      func(protectionrepository.OperationLockModel) error
	validationErr error
}

func (r *fakeReconciler) Reconcile(_ context.Context, operation protectionrepository.OperationLockModel) (ReconcileDecision, error) {
	r.calls++
	r.seen = append(r.seen, operation)
	if r.validate != nil {
		r.validationErr = r.validate(operation)
	}
	return r.decision, nil
}

func (r *fakeRecovery) HasMutationArtifact(context.Context, protectionrepository.OperationLockModel) (bool, error) {
	return r.hasArtifact, r.evidenceErr
}
func (r *fakeRecovery) AttemptRollback(context.Context, protectionrepository.OperationLockModel) error {
	r.rollbacks++
	return r.rollbackErr
}
func (r *fakeRecovery) CreateBundle(context.Context, protectionrepository.OperationLockModel, string) error {
	r.bundles++
	return nil
}

func TestConcurrentOperationsConflictAndIdempotencyIsSafe(t *testing.T) {
	repo := operationTestRepository(t)
	m := operationTestManager(t, repo, Options{InstanceID: "instance-one", PID: 101})

	first, err := m.Acquire(context.Background(), acquireFixture("key-one"))
	if err != nil {
		t.Fatal(err)
	}
	joined, err := m.Acquire(context.Background(), acquireFixture("key-one"))
	if err != nil {
		t.Fatalf("same idempotency key: %v", err)
	}
	if !joined.Joined || joined.Operation.OperationID != first.Operation.OperationID {
		t.Fatalf("idempotent retry = %#v, first = %#v", joined, first)
	}
	if _, err := m.Acquire(context.Background(), acquireFixture("key-two")); !errors.Is(err, ErrConflict) {
		t.Fatalf("different idempotency key error = %v", err)
	}

	const contenders = 8
	results := make(chan error, contenders)
	for i := 0; i < contenders; i++ {
		go func() {
			_, err := NewManager(repo, Options{InstanceID: newID("contender"), PID: 200}).Acquire(context.Background(), acquireFixture(newID("key")))
			results <- err
		}()
	}
	for i := 0; i < contenders; i++ {
		if err := <-results; !errors.Is(err, ErrConflict) {
			t.Fatalf("contender error = %v", err)
		}
	}
}

func TestHeartbeatRenewsLeaseWithoutRotatingFence(t *testing.T) {
	repo := operationTestRepository(t)
	m := operationTestManager(t, repo, Options{InstanceID: "heartbeat-owner", PID: 111})
	request := acquireFixture("heartbeat-stable-fence")
	request.Kind = KindPortHandoff
	acquired, err := m.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	applying, err := m.Transition(context.Background(), acquired.Operation.OperationID, acquired.Operation.Revision, StateApplying)
	if err != nil {
		t.Fatal(err)
	}
	first, err := m.Heartbeat(context.Background(), applying.OperationID, applying.Revision)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Heartbeat(context.Background(), first.OperationID, first.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != applying.Revision || second.Revision != applying.Revision {
		t.Fatalf("heartbeat rotated fence: applying=%d first=%d second=%d", applying.Revision, first.Revision, second.Revision)
	}
	if err := m.ValidateHelperLock(context.Background(), second.OperationID, "heartbeat-owner", KindPortHandoff, second.Revision); err != nil {
		t.Fatalf("stable heartbeat fence rejected: %v", err)
	}
}

func TestRecoveryBackendDispatchesByOperationKind(t *testing.T) {
	defaultRecovery := &fakeRecovery{}
	portRecovery := &fakeRecovery{}
	m := NewManager(nil, Options{Recovery: defaultRecovery})
	if err := m.SetRecoveryForKind(KindPortHandoff, portRecovery); err != nil {
		t.Fatal(err)
	}
	if m.recoveryForKind(KindPortHandoff) != portRecovery {
		t.Fatal("port handoff did not receive its kind-scoped recovery backend")
	}
	if m.recoveryForKind(KindFirewall) != defaultRecovery {
		t.Fatal("default firewall recovery backend changed")
	}
	m.started = true
	if err := m.SetRecoveryForKind(KindPortHandoff, &fakeRecovery{}); err != nil {
		t.Fatalf("idempotent kind registration while running: %v", err)
	}
	if err := m.SetRecoveryForKind(KindFronting, &fakeRecovery{}); err == nil {
		t.Fatal("new kind recovery backend was installed while running")
	}
}

func TestNativeReconcilerOwnsProcessGatePersistedFenceAndHistoricalAppliedReverify(t *testing.T) {
	repo := operationTestRepository(t)
	owner := operationTestManager(t, repo, Options{InstanceID: "native-owner", PID: 261})
	request := acquireFixture("native-applied")
	request.Kind = KindNativeFallback
	request.PlanRevision = strings.Repeat("a", 64)
	acquired, err := owner.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	applying, err := owner.Transition(context.Background(), acquired.Operation.OperationID, acquired.Operation.Revision, StateApplying)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := owner.Transition(context.Background(), applying.OperationID, applying.Revision, StateApplied)
	if err != nil {
		t.Fatal(err)
	}

	reconciler := &fakeReconciler{decision: ReconcileDecision{State: StateApplied, Reason: "freshly_reverified"}}
	recoverer := operationTestManager(t, repo, Options{InstanceID: "native-recoverer", PID: 262})
	reconciler.validate = func(operation protectionrepository.OperationLockModel) error {
		if err := recoverer.ValidateHelperLock(context.Background(), operation.OperationID, "native-recoverer", KindNativeFallback, operation.Revision); !errors.Is(err, ErrFenced) {
			return errors.New("terminal mutation lock remained authorized during reconciliation")
		}
		return recoverer.ValidateHelperReadLock(context.Background(), operation.OperationID, "native-recoverer", KindNativeFallback, operation.Revision)
	}
	if err := recoverer.SetReconcilerForKind(KindNativeFallback, reconciler); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		results, recoverErr := recoverer.Recover(context.Background())
		if recoverErr != nil || len(results) != 1 || results[0].FromState != StateApplied || results[0].ToState != StateApplied {
			t.Fatalf("attempt=%d results=%#v err=%v", attempt, results, recoverErr)
		}
	}
	if reconciler.calls != 2 || len(reconciler.seen) != 2 || reconciler.seen[0].Revision <= applied.Revision ||
		reconciler.seen[0].LockedByInstanceID != "native-recoverer" || reconciler.seen[0].LockedByPID == nil || *reconciler.seen[0].LockedByPID != 262 {
		t.Fatalf("reconciler calls=%d seen=%#v applied=%#v", reconciler.calls, reconciler.seen, applied)
	}
	if reconciler.validationErr != nil {
		t.Fatalf("reclaimed applied operation could not perform typed helper verification: %v", reconciler.validationErr)
	}
	if err := recoverer.ValidateHelperLock(context.Background(), applied.OperationID, "native-recoverer", KindNativeFallback, reconciler.seen[1].Revision); !errors.Is(err, ErrFenced) {
		t.Fatalf("applied operation remained helper-authorized after reconciliation: %v", err)
	}
	if err := recoverer.ValidateHelperReadLock(context.Background(), applied.OperationID, "native-recoverer", KindNativeFallback, reconciler.seen[1].Revision); !errors.Is(err, ErrFenced) {
		t.Fatalf("applied operation retained read authorization after reconciliation: %v", err)
	}
}

func TestKindReconcilerReclaimsReconcileRequiredForFreshResolution(t *testing.T) {
	repo := operationTestRepository(t)
	owner := operationTestManager(t, repo, Options{InstanceID: "reconcile-owner", PID: 271})
	request := acquireFixture("reconcile-required")
	request.Kind = KindFronting
	acquired, err := owner.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Transition(context.Background(), acquired.Operation.OperationID, acquired.Operation.Revision, StateReconcileRequired); err != nil {
		t.Fatal(err)
	}
	recoverer := operationTestManager(t, repo, Options{InstanceID: "reconcile-recoverer", PID: 272})
	reconciler := &fakeReconciler{decision: ReconcileDecision{State: StateCancelled, Reason: "ambiguity_resolved_before_mutation"}}
	reconciler.validate = func(operation protectionrepository.OperationLockModel) error {
		if err := recoverer.ValidateHelperLock(context.Background(), operation.OperationID, "reconcile-recoverer", KindFronting, operation.Revision); !errors.Is(err, ErrFenced) {
			return errors.New("terminal mutation lock remained authorized during reconciliation")
		}
		return recoverer.ValidateHelperReadLock(context.Background(), operation.OperationID, "reconcile-recoverer", KindFronting, operation.Revision)
	}
	if err := recoverer.SetReconcilerForKind(KindFronting, reconciler); err != nil {
		t.Fatal(err)
	}
	results, err := recoverer.Recover(context.Background())
	if err != nil || len(results) != 1 || results[0].FromState != StateReconcileRequired || results[0].ToState != StateCancelled || reconciler.calls != 1 || reconciler.validationErr != nil {
		t.Fatalf("results=%#v calls=%d err=%v", results, reconciler.calls, err)
	}
}

func TestRecoveryStaleHeartbeatWithLiveAndAbsentPID(t *testing.T) {
	t.Run("live pid becomes suspect and remains blocking", func(t *testing.T) {
		repo := operationTestRepository(t)
		oldNow := time.Unix(1_700_000_000, 0)
		owner := operationTestManager(t, repo, Options{InstanceID: "old", PID: 301, Now: func() time.Time { return oldNow }, Lease: time.Minute})
		acquired, err := owner.Acquire(context.Background(), acquireFixture("stale-live"))
		if err != nil {
			t.Fatal(err)
		}
		if err := owner.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		recoverer := operationTestManager(t, repo, Options{
			InstanceID: "new", PID: 302, Now: func() time.Time { return oldNow.Add(2 * time.Minute) }, PIDProbe: fakePIDProbe{alive: true},
		})
		results, err := recoverer.Recover(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 || results[0].ToState != StateLockSuspect {
			t.Fatalf("recovery = %#v", results)
		}
		if _, err := recoverer.Acquire(context.Background(), acquireFixture("must-block")); !errors.Is(err, ErrConflict) {
			t.Fatalf("suspect lock did not block: %v (operation %s)", err, acquired.Operation.OperationID)
		}
	})

	t.Run("absent pid plus expiry becomes abandoned", func(t *testing.T) {
		repo := operationTestRepository(t)
		oldNow := time.Unix(1_700_000_000, 0)
		owner := operationTestManager(t, repo, Options{InstanceID: "old", PID: 401, Now: func() time.Time { return oldNow }, Lease: time.Minute})
		if _, err := owner.Acquire(context.Background(), acquireFixture("stale-dead")); err != nil {
			t.Fatal(err)
		}
		_ = owner.Stop(context.Background())
		recoverer := operationTestManager(t, repo, Options{
			InstanceID: "new", PID: 402, Now: func() time.Time { return oldNow.Add(2 * time.Minute) }, PIDProbe: fakePIDProbe{alive: false},
		})
		results, err := recoverer.Recover(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 || results[0].ToState != StateAbandoned {
			t.Fatalf("recovery = %#v", results)
		}
		if _, err := recoverer.Acquire(context.Background(), acquireFixture("after-safe-recovery")); err != nil {
			t.Fatalf("acquire after verified abandonment: %v", err)
		}
	})
}

func TestPreparedOperationExpiresWithoutHeartbeat(t *testing.T) {
	repo := operationTestRepository(t)
	now := time.Unix(1_700_000_000, 0)
	m := operationTestManager(t, repo, Options{InstanceID: "prepared-owner", PID: 451, Now: func() time.Time { return now }, Lease: time.Second})
	acquired, err := m.Acquire(context.Background(), acquireFixture("prepared-expiry"))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	results, err := m.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ToState != StateAbandoned {
		t.Fatalf("prepared recovery = %#v", results)
	}
	item, err := repo.OperationByID(context.Background(), acquired.Operation.OperationID)
	if err != nil || item.State != StateAbandoned {
		t.Fatalf("prepared state=%#v err=%v", item, err)
	}
}

func TestPIDReuseWithDifferentInstanceIDIsSuspect(t *testing.T) {
	repo := operationTestRepository(t)
	now := time.Unix(1_700_000_000, 0)
	owner := operationTestManager(t, repo, Options{InstanceID: "old-instance", PID: 501, Now: func() time.Time { return now }, Lease: time.Second})
	if _, err := owner.Acquire(context.Background(), acquireFixture("pid-reuse")); err != nil {
		t.Fatal(err)
	}
	_ = owner.Stop(context.Background())
	recoverer := operationTestManager(t, repo, Options{
		InstanceID: "new-instance", PID: 501, Now: func() time.Time { return now.Add(time.Minute) }, PIDProbe: fakePIDProbe{alive: true},
	})
	results, err := recoverer.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ToState != StateLockSuspect || results[0].Reason != "pid_alive_or_reused" {
		t.Fatalf("PID reuse recovery = %#v", results)
	}
}

func TestRestartRecoversNonTerminalOperation(t *testing.T) {
	repo := operationTestRepository(t)
	now := time.Unix(1_700_000_000, 0)
	before := operationTestManager(t, repo, Options{InstanceID: "before-restart", PID: 601, Now: func() time.Time { return now }, Lease: time.Second})
	acquired, err := before.Acquire(context.Background(), acquireFixture("restart"))
	if err != nil {
		t.Fatal(err)
	}
	_ = before.Stop(context.Background())

	after := operationTestManager(t, repo, Options{
		InstanceID: "after-restart", PID: 602, Now: func() time.Time { return now.Add(time.Minute) }, PIDProbe: fakePIDProbe{alive: false},
		HeartbeatEvery: time.Hour, RecoveryEvery: time.Hour,
	})
	if err := after.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, err := after.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].OperationID != acquired.Operation.OperationID || items[0].State != StateAbandoned {
		t.Fatalf("recovered journal = %#v", items)
	}
}

func TestRestartRecoveryAttemptsRollbackOnceAndPreservesFailure(t *testing.T) {
	repo := operationTestRepository(t)
	now := time.Unix(1_700_000_000, 0)
	before := operationTestManager(t, repo, Options{InstanceID: "artifact-before", PID: 611, Now: func() time.Time { return now }, Lease: time.Second})
	request := acquireFixture("restart-artifact")
	request.InitialState = StateApplying
	acquired, err := before.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	_ = before.Stop(context.Background())
	recovery := &fakeRecovery{hasArtifact: true, rollbackErr: errors.New("simulated rollback failure")}
	after := operationTestManager(t, repo, Options{
		InstanceID: "artifact-after", PID: 612, Now: func() time.Time { return now.Add(time.Minute) },
		PIDProbe: fakePIDProbe{alive: false}, Recovery: recovery,
	})
	results, err := after.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[1].ToState != StateRollbackFailed || recovery.bundles != 1 {
		t.Fatalf("recovery results=%#v bundles=%d", results, recovery.bundles)
	}
	item, err := repo.OperationByID(context.Background(), acquired.Operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != StateRollbackFailed || item.RecoveryAttempts != 1 {
		t.Fatalf("persisted recovery = %#v", item)
	}
	second, err := after.Recover(context.Background())
	if err != nil || len(second) != 0 || recovery.bundles != 1 {
		t.Fatalf("rollback was retried: results=%#v err=%v bundles=%d", second, err, recovery.bundles)
	}
}

func TestRecoveryHandlesEveryRollbackEligibleState(t *testing.T) {
	for _, state := range []string{StateApplying, StateHealthFailed, StateRollingBack} {
		t.Run(state, func(t *testing.T) {
			repo := operationTestRepository(t)
			now := time.Unix(1_700_000_000, 0)
			before := operationTestManager(t, repo, Options{InstanceID: "state-before-" + state, PID: 631, Now: func() time.Time { return now }, Lease: time.Second})
			request := acquireFixture("state-" + state)
			request.InitialState = state
			if state == StateHealthFailed {
				request.InitialState = StateApplying
			}
			acquired, err := before.Acquire(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if state == StateHealthFailed {
				if _, err := before.Transition(context.Background(), acquired.Operation.OperationID, acquired.Operation.Revision, StateHealthFailed); err != nil {
					t.Fatal(err)
				}
			}
			_ = before.Stop(context.Background())
			recovery := &fakeRecovery{hasArtifact: true}
			after := operationTestManager(t, repo, Options{
				InstanceID: "state-after-" + state, PID: 632, Now: func() time.Time { return now.Add(time.Minute) },
				PIDProbe: fakePIDProbe{alive: false}, Recovery: recovery,
			})
			results, err := after.Recover(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 2 || results[1].ToState != StateRolledBack {
				t.Fatalf("state %s recovery = %#v", state, results)
			}
		})
	}
}

func TestFrontingRestartBeforeMutationBecomesCancelled(t *testing.T) {
	for _, state := range []string{StatePrepared, StateApplying} {
		t.Run(state, func(t *testing.T) {
			repo := operationTestRepository(t)
			now := time.Unix(1_700_000_000, 0)
			before := operationTestManager(t, repo, Options{InstanceID: "fronting-before-" + state, PID: 633, Now: func() time.Time { return now }, Lease: time.Second})
			request := acquireFixture("fronting-cancel-" + state)
			request.Kind, request.InitialState = KindFronting, state
			if _, err := before.Acquire(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			_ = before.Stop(context.Background())
			recovery := &fakeRecovery{hasArtifact: false}
			after := operationTestManager(t, repo, Options{InstanceID: "fronting-after-" + state, PID: 634, Now: func() time.Time { return now.Add(time.Minute) }, PIDProbe: fakePIDProbe{alive: false}})
			if err := after.SetRecoveryForKind(KindFronting, recovery); err != nil {
				t.Fatal(err)
			}
			results, err := after.Recover(context.Background())
			if err != nil || len(results) != 1 || results[0].ToState != StateCancelled || recovery.rollbacks != 0 {
				t.Fatalf("results=%#v rollbacks=%d err=%v", results, recovery.rollbacks, err)
			}
		})
	}
}

func TestRestartRecoveryChecksumFailureBecomesRollbackFailed(t *testing.T) {
	repo := operationTestRepository(t)
	now := time.Unix(1_700_000_000, 0)
	before := operationTestManager(t, repo, Options{InstanceID: "checksum-before", PID: 621, Now: func() time.Time { return now }, Lease: time.Second})
	request := acquireFixture("checksum-recovery")
	request.InitialState = StateApplying
	if _, err := before.Acquire(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	_ = before.Stop(context.Background())
	recovery := &fakeRecovery{evidenceErr: errors.New("checksum mismatch")}
	after := operationTestManager(t, repo, Options{
		InstanceID: "checksum-after", PID: 622, Now: func() time.Time { return now.Add(time.Minute) },
		PIDProbe: fakePIDProbe{alive: false}, Recovery: recovery,
	})
	results, err := after.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ToState != StateRollbackFailed || recovery.bundles != 1 {
		t.Fatalf("checksum recovery=%#v bundles=%d", results, recovery.bundles)
	}
}

func TestRestartAfterRecordedRollbackAttemptDoesNotRetryMutation(t *testing.T) {
	repo := operationTestRepository(t)
	now := time.Unix(1_700_000_000, 0)
	before := operationTestManager(t, repo, Options{InstanceID: "attempt-before", PID: 641, Now: func() time.Time { return now }, Lease: time.Second})
	request := acquireFixture("recorded-attempt")
	request.InitialState = StateRollingBack
	acquired, err := before.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkOperationRecovery(context.Background(), acquired.Operation.OperationID, 1, now.Unix(), "rollback_failed"); err != nil {
		t.Fatal(err)
	}
	_ = before.Stop(context.Background())
	recovery := &fakeRecovery{hasArtifact: true, rollbackErr: errors.New("must not be called")}
	after := operationTestManager(t, repo, Options{
		InstanceID: "attempt-after", PID: 642, Now: func() time.Time { return now.Add(time.Minute) },
		PIDProbe: fakePIDProbe{alive: false}, Recovery: recovery,
	})
	results, err := after.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ToState != StateRollbackFailed || recovery.rollbacks != 0 || recovery.bundles != 1 {
		t.Fatalf("recorded attempt recovery=%#v rollbacks=%d bundles=%d", results, recovery.rollbacks, recovery.bundles)
	}
}

func TestForceUnlockRequiresConfirmationAndRecordsAudit(t *testing.T) {
	repo := operationTestRepository(t)
	var mu sync.Mutex
	var audits []AuditEvent
	m := operationTestManager(t, repo, Options{InstanceID: "force", PID: 701, Audit: func(_ context.Context, event AuditEvent) error {
		mu.Lock()
		defer mu.Unlock()
		audits = append(audits, event)
		return nil
	}})
	acquired, err := m.Acquire(context.Background(), acquireFixture("force"))
	if err != nil {
		t.Fatal(err)
	}
	request := ForceUnlockRequest{OperationID: acquired.Operation.OperationID, Revision: acquired.Operation.Revision, Actor: "admin"}
	if _, err := m.ForceUnlock(context.Background(), request); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("unconfirmed force unlock error = %v", err)
	}
	request.Confirmed = true
	item, err := m.ForceUnlock(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != StateForceUnlocked {
		t.Fatalf("state = %s", item.State)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(audits) != 1 || audits[0].Event != "server_protection.force_unlock" || audits[0].Actor != "admin" {
		t.Fatalf("audits = %#v", audits)
	}
}

func TestForgetStateRequiresExplicitPhraseAndAudit(t *testing.T) {
	repo := operationTestRepository(t)
	var audits []AuditEvent
	m := operationTestManager(t, repo, Options{InstanceID: "forget", PID: 711, Audit: func(_ context.Context, event AuditEvent) error {
		audits = append(audits, event)
		return nil
	}})
	acquired, err := m.Acquire(context.Background(), acquireFixture("forget-state"))
	if err != nil {
		t.Fatal(err)
	}
	applying, err := m.Transition(context.Background(), acquired.Operation.OperationID, acquired.Operation.Revision, StateApplying)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := m.Transition(context.Background(), applying.OperationID, applying.Revision, StateApplied)
	if err != nil {
		t.Fatal(err)
	}
	request := ForgetStateRequest{OperationID: applied.OperationID, Revision: applied.Revision, Actor: "admin", Confirmation: "wrong"}
	if _, err := m.ForgetState(context.Background(), request); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("unconfirmed forget error = %v", err)
	}
	if len(audits) != 0 {
		t.Fatalf("unconfirmed forget was audited as accepted: %#v", audits)
	}
	request.Confirmation = "FORGET_SERVER_PROTECTION_STATE"
	forgotten, err := m.ForgetState(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if forgotten.State != StateForgotten || len(audits) != 1 || audits[0].Event != "server_protection.forget_state" {
		t.Fatalf("forgotten=%#v audits=%#v", forgotten, audits)
	}
}

func TestOldInstanceCannotFinishOrUnlockNewInstanceOperation(t *testing.T) {
	repo := operationTestRepository(t)
	old := operationTestManager(t, repo, Options{InstanceID: "old", PID: 801})
	first, err := old.Acquire(context.Background(), acquireFixture("old-op"))
	if err != nil {
		t.Fatal(err)
	}
	applying, err := old.Transition(context.Background(), first.Operation.OperationID, first.Operation.Revision, StateApplying)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Transition(context.Background(), applying.OperationID, applying.Revision, StateApplied); err != nil {
		t.Fatal(err)
	}
	newOwner := operationTestManager(t, repo, Options{InstanceID: "new", PID: 802})
	second, err := newOwner.Acquire(context.Background(), acquireFixture("new-op"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.UpdateOperationLockFenced(context.Background(), protectionrepository.FencedOperationLockUpdate{
		OperationID: second.Operation.OperationID, Revision: second.Operation.Revision,
		InstanceID: "old", PID: 801, FromStates: protectionrepository.NonTerminalOperationLockStates(),
		ToState: StateApplied, Now: time.Now().Unix(), ExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	if !errors.Is(err, protectionrepository.ErrOperationFenced) {
		t.Fatalf("old instance update error = %v", err)
	}
	if _, err := old.Transition(context.Background(), second.Operation.OperationID, second.Operation.Revision, StateApplied); !errors.Is(err, ErrFenced) {
		t.Fatalf("old manager finished new operation: %v", err)
	}
	if _, err := old.ForceUnlock(context.Background(), ForceUnlockRequest{
		OperationID: second.Operation.OperationID, Revision: second.Operation.Revision, Actor: "admin", Confirmed: true,
	}); !errors.Is(err, ErrFenced) {
		t.Fatalf("old manager force-unlocked new operation: %v", err)
	}
}

func TestTerminalAndNonTerminalTransitionsAreValidated(t *testing.T) {
	repo := operationTestRepository(t)
	m := operationTestManager(t, repo, Options{InstanceID: "transitions", PID: 901})
	result, err := m.Acquire(context.Background(), acquireFixture("transitions"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Transition(context.Background(), result.Operation.OperationID, result.Operation.Revision, StateRolledBack); !errors.Is(err, protectionrepository.ErrOperationFenced) {
		t.Fatalf("invalid transition error = %v", err)
	}
	applying, err := m.Transition(context.Background(), result.Operation.OperationID, result.Operation.Revision, StateApplying)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := m.Transition(context.Background(), applying.OperationID, applying.Revision, StateApplied)
	if err != nil {
		t.Fatal(err)
	}
	if applied.State != StateApplied {
		t.Fatalf("terminal state = %s", applied.State)
	}
	if _, err := m.Heartbeat(context.Background(), applied.OperationID, applied.Revision); !errors.Is(err, protectionrepository.ErrOperationFenced) {
		t.Fatalf("terminal heartbeat error = %v", err)
	}
}

func TestHelperLockValidationUsesPersistedFencing(t *testing.T) {
	repo := operationTestRepository(t)
	m := operationTestManager(t, repo, Options{InstanceID: "helper-owner", PID: 910})
	request := acquireFixture("helper-lock")
	request.Kind = KindFronting
	acquired, err := m.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	operation := acquired.Operation
	if err := m.ValidateHelperLock(context.Background(), operation.OperationID, "helper-owner", KindFronting, operation.Revision); err != nil {
		t.Fatalf("current persisted lock was rejected: %v", err)
	}
	for name, validate := range map[string]func() error{
		"wrong instance": func() error {
			return m.ValidateHelperLock(context.Background(), operation.OperationID, "other", KindFronting, operation.Revision)
		},
		"wrong kind": func() error {
			return m.ValidateHelperLock(context.Background(), operation.OperationID, "helper-owner", KindFirewall, operation.Revision)
		},
		"stale revision": func() error {
			return m.ValidateHelperLock(context.Background(), operation.OperationID, "helper-owner", KindFronting, operation.Revision+1)
		},
	} {
		if err := validate(); !errors.Is(err, ErrFenced) {
			t.Fatalf("%s was not fenced: %v", name, err)
		}
	}
}

func operationTestRepository(t *testing.T) *protectionrepository.Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "operations.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := protectionrepository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return protectionrepository.New(db)
}

func operationTestManager(t *testing.T, repo *protectionrepository.Repository, options Options) *Manager {
	t.Helper()
	if options.HeartbeatEvery == 0 {
		options.HeartbeatEvery = time.Hour
	}
	if options.RecoveryEvery == 0 {
		options.RecoveryEvery = time.Hour
	}
	m := NewManager(repo, options)
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	return m
}

func acquireFixture(key string) AcquireRequest {
	port := 443
	return AcquireRequest{
		Kind: KindFirewall, ResourceID: "panel:https", Protocol: "tcp", Listen: "0.0.0.0",
		Port: &port, IdempotencyKey: key, Actor: "admin",
	}
}
