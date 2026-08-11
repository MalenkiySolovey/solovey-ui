package handoff

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type deadPID struct{}

func (deadPID) Alive(int) (bool, error) { return false, nil }

type fixture struct {
	t        *testing.T
	db       *gorm.DB
	repo     *protectionrepository.Repository
	svc      *Service
	manager  *protectionoperations.Manager
	inbound  *MockInbound
	fallback *MockFallback
	restart  *MockRestart
	owners   *MockOwnership
	health   *MockHealth
	snapshot *MockSnapshot
	listener *MockListener
	recovery *MockRecoveryUX
	now      time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "port.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&protectionrepository.OperationLockModel{}, &protectionrepository.PortOperationModel{}, &protectionrepository.ArtifactModel{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	repo := protectionrepository.New(db)
	manager := protectionoperations.NewManager(repo, protectionoperations.Options{InstanceID: "test-instance", PID: 42, Now: func() time.Time { return now }, PIDProbe: deadPID{}, HeartbeatEvery: 5 * time.Millisecond, RecoveryEvery: time.Hour, Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil }})
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	previous := samplePrevious()
	owners := &MockOwnership{CurrentOwner: previous}
	inbound, fallback, restart, health, snapshot, listener, recovery := &MockInbound{}, &MockFallback{}, &MockRestart{}, &MockHealth{}, &MockSnapshot{}, &MockListener{}, &MockRecoveryUX{}
	inbound.OnCall = map[string]func(){
		"apply":    func() { owners.SetCurrent(sampleNext()) },
		"rollback": func() { owners.SetCurrent(samplePrevious()) },
	}
	svc := NewService(repo, manager)
	svc.Inbound, svc.Fallback, svc.Restart, svc.Ownership, svc.Health = inbound, fallback, restart, owners, health
	svc.Helper, svc.Snapshot, svc.Listener, svc.Recovery, svc.Now = MockHelper{Value: DefaultMockCapabilities()}, snapshot, listener, recovery, func() time.Time { return now }
	return &fixture{t: t, db: db, repo: repo, svc: svc, manager: manager, inbound: inbound, fallback: fallback, restart: restart, owners: owners, health: health, snapshot: snapshot, listener: listener, recovery: recovery, now: now}
}

func samplePrevious() OwnerSnapshot {
	return OwnerSnapshot{ResourceID: "fixture:site-1", Owner: "fixture-provider", Kind: "public_site", Protocol: "tcp", Listen: "127.0.0.1", Port: 443, ResourceRevision: "resource-1", ConfigRevision: "config-1", Fingerprint: "previous-fingerprint"}
}
func sampleNext() OwnerSnapshot {
	return OwnerSnapshot{ResourceID: "inbound:reality-1", Owner: "core", Kind: "inbound", Protocol: "tcp", Listen: "127.0.0.1", Port: 443, ResourceRevision: "resource-2", ConfigRevision: "config-2", Fingerprint: "next-fingerprint", Profile: Profile{Protocol: "vless", Security: "reality", StrictSNI: true, HandshakeHost: "example.test", FallbackListen: "127.0.0.1", FallbackPort: 8443}}
}
func samplePlan() Plan {
	return Plan{PlanRevision: "plan-1", IdempotencyKey: "idempotency-1", Actor: "admin", Previous: samplePrevious(), Next: sampleNext()}
}
func prepare(t *testing.T, f *fixture, plan Plan) protectionrepository.PortOperationModel {
	t.Helper()
	item, _, err := f.svc.Prepare(context.Background(), plan, "PREPARE PORT HANDOFF "+plan.PlanRevision)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestPortHandoffHappyPathPersistsImmutableSnapshots(t *testing.T) {
	f := newFixture(t)
	plan := samplePlan()
	item := prepare(t, f, plan)
	plan.Previous.Owner, plan.Next.Profile.HandshakeHost = "mutated", "wrong.test"
	result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
	if err != nil || result.State != StateApplied {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	persisted, err := f.repo.PortOperation(context.Background(), item.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	previous, next, err := snapshots(persisted)
	if err != nil {
		t.Fatal(err)
	}
	if previous.Owner != "fixture-provider" || next.Profile.HandshakeHost != "example.test" || persisted.PreviousResourceRevision != "resource-1" || persisted.NextConfigRevision != "config-2" {
		t.Fatalf("immutable journal=%#v %#v", persisted, next)
	}
	if len(f.inbound.Calls) != 2 || f.restart.Calls != 1 || len(f.fallback.Calls) != 2 {
		t.Fatalf("unexpected mock calls inbound=%v fallback=%v restart=%d", f.inbound.Calls, f.fallback.Calls, f.restart.Calls)
	}
	if len(f.snapshot.Calls) != 2 || len(f.listener.Calls) != 1 || f.listener.Calls[0] != sampleNext().ResourceID {
		t.Fatalf("snapshot/listener choreography=%v %v", f.snapshot.Calls, f.listener.Calls)
	}
}

func TestPortHandoffRejectsCollisionStaleOwnerAndFencing(t *testing.T) {
	t.Run("collision", func(t *testing.T) {
		f := newFixture(t)
		f.owners.AddOwner(OwnerSnapshot{ResourceID: "panel:web", Owner: "panel", Protocol: "tcp", Listen: "127.0.0.1", Port: 443})
		_, _, err := f.svc.Prepare(context.Background(), samplePlan(), "PREPARE PORT HANDOFF plan-1")
		if !errors.Is(err, ErrCollision) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("stale_revision_and_fingerprint", func(t *testing.T) {
		f := newFixture(t)
		item := prepare(t, f, samplePlan())
		stale := samplePrevious()
		stale.ConfigRevision, stale.Fingerprint = "config-new", "fingerprint-new"
		f.owners.SetCurrent(stale)
		_, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("err=%v", err)
		}
		if err := f.inbound.RequireNoCalls(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("fencing_mismatch", func(t *testing.T) {
		f := newFixture(t)
		item := prepare(t, f, samplePlan())
		lock, err := f.manager.List(context.Background())
		if err != nil || len(lock) != 1 {
			t.Fatal(err)
		}
		if _, err := f.manager.Transition(context.Background(), item.OperationID, lock[0].Revision, StateApplying); err != nil {
			t.Fatal(err)
		}
		_, err = f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if !errors.Is(err, protectionoperations.ErrFenced) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestPortHandoffIdempotencyAndCancellationBoundaries(t *testing.T) {
	t.Run("idempotent_retry", func(t *testing.T) {
		f := newFixture(t)
		item := prepare(t, f, samplePlan())
		retry, joined, err := f.svc.Prepare(context.Background(), samplePlan(), "PREPARE PORT HANDOFF plan-1")
		if err != nil || !joined || retry.OperationID != item.OperationID {
			t.Fatalf("retry=%#v joined=%v err=%v", retry, joined, err)
		}
		one, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if err != nil || one.State != StateApplied {
			t.Fatal(err)
		}
		two, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if err != nil || two.State != StateApplied {
			t.Fatal(err)
		}
	})
	t.Run("before_mutation", func(t *testing.T) {
		f := newFixture(t)
		item := prepare(t, f, samplePlan())
		cancelled, err := f.svc.Cancel(context.Background(), item.OperationID)
		if err != nil || cancelled.State != StateCancelled {
			t.Fatalf("cancelled=%#v err=%v", cancelled, err)
		}
		_, err = f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.inbound.RequireNoCalls(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("after_mutation_boundary_rolls_back", func(t *testing.T) {
		f := newFixture(t)
		item := prepare(t, f, samplePlan())
		f.fallback.OnCall = map[string]func(){"apply": func() { _, _ = f.svc.Cancel(context.Background(), item.OperationID) }}
		result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if !errors.Is(err, ErrCancelled) || result.State != StateRolledBack {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})
}

func TestPortHandoffHealthRollbackAndOwnerLoss(t *testing.T) {
	t.Run("health_failure_rolls_back", func(t *testing.T) {
		f := newFixture(t)
		item := prepare(t, f, samplePlan())
		f.health.Results = []HealthResult{{OK: false, Fact: "mock_failed"}}
		result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if !errors.Is(err, ErrHealth) || result.State != StateRollbackFailed {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})
	t.Run("rollback_failure_has_recovery_state", func(t *testing.T) {
		f := newFixture(t)
		item := prepare(t, f, samplePlan())
		f.inbound.Err = map[string]error{"apply": errors.New("apply failed"), "rollback": errors.New("rollback failed")}
		result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if result.State != StateRollbackFailed || err == nil || len(f.recovery.Calls) != 1 {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})
	t.Run("owner_disappeared", func(t *testing.T) {
		f := newFixture(t)
		item := prepare(t, f, samplePlan())
		f.owners.Missing = true
		result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if !errors.Is(err, ErrOwnerDisappeared) || result.State != StateAbandoned {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})
}

func TestPortHandoffValidatesWildcardACMEProfilesAndCapabilities(t *testing.T) {
	t.Run("wildcard_and_dual_stack", func(t *testing.T) {
		f := newFixture(t)
		plan := samplePlan()
		plan.Previous.Listen, plan.Next.Listen = "0.0.0.0", "0.0.0.0"
		_, _, err := f.svc.Prepare(context.Background(), plan, "PREPARE PORT HANDOFF plan-1")
		if !errors.Is(err, ErrWildcardConfirm) {
			t.Fatalf("err=%v", err)
		}
		f = newFixture(t)
		f.owners.AddOwner(OwnerSnapshot{ResourceID: "ipv6", Owner: "other", Protocol: "tcp", Listen: "::", Port: 443})
		_, _, err = f.svc.Prepare(context.Background(), samplePlan(), "PREPARE PORT HANDOFF plan-1")
		if !errors.Is(err, ErrCollision) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("acme", func(t *testing.T) {
		f := newFixture(t)
		plan := samplePlan()
		plan.Previous.ACMERenewal, plan.Previous.ReservedRoutes = true, []string{"/.well-known/acme-challenge/"}
		_, _, err := f.svc.Prepare(context.Background(), plan, "PREPARE PORT HANDOFF plan-1")
		if !errors.Is(err, ErrACMEConflict) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("vless_reality", func(t *testing.T) {
		f := newFixture(t)
		plan := samplePlan()
		plan.Next.Profile.StrictSNI = false
		_, _, err := f.svc.Prepare(context.Background(), plan, "PREPARE PORT HANDOFF plan-1")
		if !errors.Is(err, ErrInvalidPlan) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("trojan", func(t *testing.T) {
		f := newFixture(t)
		plan := samplePlan()
		plan.Next.Profile = Profile{Protocol: "trojan", Security: "tls", CertificateRef: "cert-ref-only", FallbackListen: "127.0.0.1", FallbackPort: 8443, ALPNFallbacks: map[string]string{"h3": "bad"}}
		_, _, err := f.svc.Prepare(context.Background(), plan, "PREPARE PORT HANDOFF plan-1")
		if !errors.Is(err, ErrInvalidPlan) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("unsupported_and_proxy", func(t *testing.T) {
		f := newFixture(t)
		plan := samplePlan()
		plan.Next.Profile.Protocol = "shadowsocks"
		_, _, err := f.svc.Prepare(context.Background(), plan, "PREPARE PORT HANDOFF plan-1")
		if !errors.Is(err, ErrProtocol) {
			t.Fatalf("err=%v", err)
		}
		f = newFixture(t)
		plan = samplePlan()
		plan.Next.ProxyProtocol = true
		caps := DefaultMockCapabilities()
		caps.ProxyProtocol = false
		f.svc.Helper = MockHelper{Value: caps}
		_, _, err = f.svc.Prepare(context.Background(), plan, "PREPARE PORT HANDOFF plan-1")
		if !errors.Is(err, ErrProxyCapability) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestHandoffSafetyBlockers(t *testing.T) {
	t.Run("critical_owner_refused", func(t *testing.T) {
		f := newFixture(t)
		plan := samplePlan()
		plan.Previous.Kind = "panel_web"
		if _, _, err := f.svc.Prepare(context.Background(), plan, "PREPARE PORT HANDOFF plan-1"); !errors.Is(err, ErrCriticalOwner) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("advanced_wildcard_refused_before_apply", func(t *testing.T) {
		f := newFixture(t)
		plan := samplePlan()
		plan.Previous.Listen, plan.Next.Listen = "0.0.0.0", "0.0.0.0"
		plan.AdvancedConfirmed, plan.AdvancedPhrase = true, "ALLOW WILDCARD LISTENER plan-1"
		if _, _, err := f.svc.Prepare(context.Background(), plan, "PREPARE PORT HANDOFF plan-1"); !errors.Is(err, ErrExactListener) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("proxy_mismatch_refused", func(t *testing.T) {
		f := newFixture(t)
		plan := samplePlan()
		plan.Next.ProxyProtocol = true
		if _, _, err := f.svc.Prepare(context.Background(), plan, "PREPARE PORT HANDOFF plan-1"); !errors.Is(err, ErrProxyCapability) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("stale_owner_after_prepare_never_releases_source", func(t *testing.T) {
		f := newFixture(t)
		item := prepare(t, f, samplePlan())
		stale := samplePrevious()
		stale.ConfigRevision = "config-after-prepare"
		f.fallback.OnCall = map[string]func(){"prepare": func() { f.owners.SetCurrent(stale) }}
		result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if !errors.Is(err, ErrRevisionConflict) || result.State != StatePrepared {
			t.Fatalf("result=%#v err=%v", result, err)
		}
		for _, call := range f.fallback.Calls {
			if call == "apply" {
				t.Fatalf("source released after stale recheck: %v", f.fallback.Calls)
			}
		}
	})

	t.Run("incomplete_health_cannot_commit", func(t *testing.T) {
		f := newFixture(t)
		item := prepare(t, f, samplePlan())
		f.health.Sequence = [][]HealthResult{
			{{Target: HealthTarget{ResourceID: sampleNext().ResourceID, Check: "next_owner"}, OK: true, Fact: "listener_ready"}},
			{{Target: HealthTarget{ResourceID: samplePrevious().ResourceID, Check: "rollback_previous_owner"}, OK: true, Fact: "listener_ready"}},
		}
		result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if !errors.Is(err, ErrHealth) || result.State != StateRolledBack {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})

	t.Run("core_workflow_restart_is_not_duplicated", func(t *testing.T) {
		f := newFixture(t)
		f.inbound.ApplyRestarted = true
		item := prepare(t, f, samplePlan())
		result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if err != nil || result.State != StateApplied || f.restart.Calls != 0 {
			t.Fatalf("result=%#v restarts=%d err=%v", result, f.restart.Calls, err)
		}
	})

	t.Run("heartbeat_revision_refreshes_before_each_boundary", func(t *testing.T) {
		f := newFixture(t)
		if err := f.manager.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		f.fallback.OnCall = map[string]func(){"apply": func() { time.Sleep(30 * time.Millisecond) }}
		item := prepare(t, f, samplePlan())
		result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if err != nil || result.State != StateApplied {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})

	t.Run("rollback_restart_is_not_duplicated", func(t *testing.T) {
		f := newFixture(t)
		f.inbound.ApplyRestarted = true
		f.inbound.RollbackRestarted = true
		item := prepare(t, f, samplePlan())
		f.health.Sequence = [][]HealthResult{
			{{Target: HealthTarget{ResourceID: sampleNext().ResourceID, Check: "next_owner"}, OK: true, Fact: "listener_ready"}},
			{{Target: HealthTarget{ResourceID: samplePrevious().ResourceID, Check: "rollback_previous_owner"}, OK: true, Fact: "listener_ready"}},
		}
		result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if !errors.Is(err, ErrHealth) || result.State != StateRolledBack || f.restart.Calls != 0 {
			t.Fatalf("result=%#v restarts=%d err=%v", result, f.restart.Calls, err)
		}
	})

	t.Run("wrong_runtime_owner_rolls_back", func(t *testing.T) {
		f := newFixture(t)
		f.inbound.OnCall = map[string]func(){"rollback": func() { f.owners.SetCurrent(samplePrevious()) }}
		item := prepare(t, f, samplePlan())
		result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if !errors.Is(err, ErrListenerVerify) || result.State != StateRolledBack {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})

	t.Run("cancelled_request_uses_detached_rollback", func(t *testing.T) {
		f := newFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		adapter := &cancelOnApplyInbound{base: f.inbound, owners: f.owners, cancel: cancel}
		f.svc.Inbound = adapter
		item := prepare(t, f, samplePlan())
		result, err := f.svc.Apply(ctx, item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
		if !errors.Is(err, context.Canceled) || result.State != StateRolledBack || !adapter.rollbackContextLive {
			t.Fatalf("result=%#v rollbackContextLive=%v err=%v", result, adapter.rollbackContextLive, err)
		}
	})

	for _, state := range []string{StateHealth, StateApplied} {
		t.Run("journal_failure_"+state+"_restores_previous_owner", func(t *testing.T) {
			f := newFixture(t)
			f.svc.Journal = &failingJournal{JournalStore: f.repo, failState: state}
			item := prepare(t, f, samplePlan())
			result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
			if err == nil || result.State != StateRolledBack {
				t.Fatalf("result=%#v err=%v", result, err)
			}
		})
	}
}

type cancelOnApplyInbound struct {
	base                *MockInbound
	owners              *MockOwnership
	cancel              context.CancelFunc
	rollbackContextLive bool
}

type failingJournal struct {
	JournalStore
	failState string
	failed    bool
}

func (j *failingJournal) UpdatePortOperationFenced(ctx context.Context, update protectionrepository.FencedPortOperationUpdate) (protectionrepository.PortOperationModel, error) {
	if !j.failed && update.ToState == j.failState {
		j.failed = true
		return protectionrepository.PortOperationModel{}, errors.New("injected journal failure")
	}
	return j.JournalStore.UpdatePortOperationFenced(ctx, update)
}

func (a *cancelOnApplyInbound) Prepare(ctx context.Context, next OwnerSnapshot, fence Fence) error {
	return a.base.Prepare(ctx, next, fence)
}
func (a *cancelOnApplyInbound) AbortPrepare(ctx context.Context, next OwnerSnapshot, fence Fence) error {
	return a.base.AbortPrepare(ctx, next, fence)
}
func (a *cancelOnApplyInbound) Apply(ctx context.Context, _ OwnerSnapshot, _ Fence) (CoreMutationResult, error) {
	a.cancel()
	return CoreMutationResult{}, ctx.Err()
}
func (a *cancelOnApplyInbound) Rollback(ctx context.Context, _, _ OwnerSnapshot, _ Fence) (CoreMutationResult, error) {
	a.rollbackContextLive = ctx.Err() == nil
	a.owners.SetCurrent(samplePrevious())
	return CoreMutationResult{}, ctx.Err()
}

func TestPortHandoffRestartRecoveryForEveryNonTerminalState(t *testing.T) {
	for _, state := range []string{StatePrepared, StateApplying, StateHealth, StateHealthFailed, StateRollingBack} {
		t.Run(state, func(t *testing.T) {
			f := newFixture(t)
			item := prepare(t, f, samplePlan())
			advance(t, f, &item, state)
			if err := f.db.Model(&protectionrepository.OperationLockModel{}).Where("operation_id = ?", item.OperationID).Updates(map[string]any{"expires_at": f.now.Add(-time.Second).Unix(), "locked_by_pid": 999}).Error; err != nil {
				t.Fatal(err)
			}
			if err := f.manager.Stop(context.Background()); err != nil {
				t.Fatal(err)
			}
			manager := protectionoperations.NewManager(f.repo, protectionoperations.Options{InstanceID: "restarted", PID: 77, Now: func() time.Time { return f.now }, PIDProbe: deadPID{}, Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil }})
			svc := NewService(f.repo, manager)
			svc.Inbound, svc.Fallback, svc.Restart, svc.Ownership, svc.Health, svc.Helper, svc.Snapshot, svc.Listener, svc.Recovery, svc.Now = f.inbound, f.fallback, f.restart, f.owners, f.health, MockHelper{Value: DefaultMockCapabilities()}, f.snapshot, f.listener, f.recovery, func() time.Time { return f.now }
			if _, err := svc.Recover(context.Background()); err != nil {
				t.Fatal(err)
			}
			final, err := f.repo.PortOperation(context.Background(), item.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			want := StateRolledBack
			if state == StatePrepared {
				want = StateAbandoned
			}
			if final.State != want {
				t.Fatalf("recovered=%s want=%s", final.State, want)
			}
		})
	}
}

func advance(t *testing.T, f *fixture, item *protectionrepository.PortOperationModel, to string) {
	t.Helper()
	sequence := []string{StateApplying, StateHealth, StateHealthFailed, StateRollingBack}
	for _, state := range sequence {
		if item.State == to {
			return
		}
		locks, err := f.manager.List(context.Background())
		if err != nil || len(locks) != 1 {
			t.Fatal(err)
		}
		lock, err := f.manager.Transition(context.Background(), item.OperationID, locks[0].Revision, state)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := f.svc.transitionJournal(context.Background(), *item, []string{item.State}, state, nil)
		if err != nil {
			t.Fatal(err)
		}
		*item = updated
		if state == StateApplying {
			f.snapshot.Mutation = true
		}
		_ = lock
	}
}
