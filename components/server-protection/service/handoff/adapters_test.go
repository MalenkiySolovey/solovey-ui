package handoff

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
)

type injectedAdapters struct {
	trace    []string
	failAt   string
	failed   bool
	mutation bool
	owners   *MockOwnership
}

func (a *injectedAdapters) step(name string) error {
	a.trace = append(a.trace, name)
	if !a.failed && a.failAt == name {
		a.failed = true
		return errors.New("injected_" + name)
	}
	if a.owners != nil {
		if name == "target_activate" {
			a.owners.SetCurrent(sampleNext())
		}
		if name == "inbound_restore" {
			a.owners.SetCurrent(samplePrevious())
		}
	}
	return nil
}
func (a *injectedAdapters) Prepare(context.Context, OwnerSnapshot, Fence) error {
	return a.step("inbound_prepare")
}
func (a *injectedAdapters) AbortPrepare(context.Context, OwnerSnapshot, Fence) error {
	return a.step("inbound_abort_prepare")
}
func (a *injectedAdapters) Apply(context.Context, OwnerSnapshot, Fence) (CoreMutationResult, error) {
	return CoreMutationResult{}, a.step("target_activate")
}
func (a *injectedAdapters) Rollback(context.Context, OwnerSnapshot, OwnerSnapshot, Fence) (CoreMutationResult, error) {
	return CoreMutationResult{}, a.step("inbound_restore")
}
func (a *injectedAdapters) Restart(context.Context, Fence) error { return a.step("singbox_restart") }
func (a *injectedAdapters) Checkpoint(context.Context, OwnerSnapshot, OwnerSnapshot, Fence) error {
	return a.step("durable_checkpoint")
}
func (a *injectedAdapters) MarkMutation(context.Context, Fence) error {
	err := a.step("mutation_marker")
	if err == nil {
		a.mutation = true
	}
	return err
}
func (a *injectedAdapters) HasMutation(context.Context, Fence) (bool, error) { return a.mutation, nil }
func (a *injectedAdapters) Verify(_ context.Context, owner OwnerSnapshot, _ Fence) error {
	if owner.ResourceID == samplePrevious().ResourceID {
		return a.step("previous_listener_verify")
	}
	return a.step("target_listener_verify")
}
func (a *injectedAdapters) Check(_ context.Context, targets []HealthTarget) ([]HealthResult, error) {
	name := "bounded_health"
	if len(targets) == 1 && targets[0].Check == "rollback_previous_owner" {
		name = "rollback_health"
	}
	if err := a.step(name); err != nil {
		return nil, err
	}
	results := make([]HealthResult, 0, len(targets))
	for _, target := range targets {
		results = append(results, HealthResult{Target: target, OK: true, Fact: "fake_ok"})
	}
	return results, nil
}

type injectedFallback struct{ set *injectedAdapters }

func (a injectedFallback) Prepare(context.Context, OwnerSnapshot, Fence) error {
	return a.set.step("target_prepare")
}
func (a injectedFallback) AbortPrepare(context.Context, OwnerSnapshot, Fence) error {
	return a.set.step("target_abort_prepare")
}
func (a injectedFallback) Apply(context.Context, OwnerSnapshot, Fence) error {
	return a.set.step("source_release")
}
func (a injectedFallback) Rollback(context.Context, OwnerSnapshot, Fence) error {
	return a.set.step("previous_owner_restore")
}

func installInjected(f *fixture, failAt string) *injectedAdapters {
	set := &injectedAdapters{failAt: failAt, owners: f.owners}
	f.svc.Inbound, f.svc.Fallback, f.svc.Restart = set, injectedFallback{set}, set
	f.svc.Snapshot, f.svc.Listener, f.svc.Health = set, set, set
	return set
}

func TestAdapterContractOrdersEveryMutationBoundary(t *testing.T) {
	f := newFixture(t)
	set := installInjected(f, "")
	item := prepare(t, f, samplePlan())
	result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
	if err != nil || result.State != StateApplied {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	want := []string{"durable_checkpoint", "inbound_prepare", "target_prepare", "mutation_marker", "source_release", "target_activate", "singbox_restart", "target_listener_verify", "bounded_health"}
	if !reflect.DeepEqual(set.trace, want) {
		t.Fatalf("trace=%v want=%v", set.trace, want)
	}
}

func TestFailureInjectionRestoresAndVerifiesPreviousOwner(t *testing.T) {
	for _, failAt := range []string{"mutation_marker", "source_release", "target_activate", "singbox_restart", "target_listener_verify", "bounded_health"} {
		t.Run(failAt, func(t *testing.T) {
			f := newFixture(t)
			set := installInjected(f, failAt)
			item := prepare(t, f, samplePlan())
			result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
			if err == nil || result.State != StateRolledBack {
				t.Fatalf("result=%#v err=%v trace=%v", result, err, set.trace)
			}
			wantTail := []string{"inbound_restore", "previous_owner_restore", "singbox_restart", "previous_listener_verify", "rollback_health"}
			if len(set.trace) < len(wantTail) || !reflect.DeepEqual(set.trace[len(set.trace)-len(wantTail):], wantTail) {
				t.Fatalf("trace=%v", set.trace)
			}
		})
	}
}

func TestFailureBeforeCheckpointNeverReleasesSource(t *testing.T) {
	for _, failAt := range []string{"durable_checkpoint", "inbound_prepare", "target_prepare"} {
		t.Run(failAt, func(t *testing.T) {
			f := newFixture(t)
			set := installInjected(f, failAt)
			item := prepare(t, f, samplePlan())
			result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
			if err == nil || result.State != StatePrepared {
				t.Fatalf("result=%#v err=%v trace=%v", result, err, set.trace)
			}
			for _, call := range set.trace {
				if call == "source_release" {
					t.Fatalf("source released before safe checkpoint: %v", set.trace)
				}
			}
		})
	}
}

func TestRollbackVerificationFailureRequiresRecovery(t *testing.T) {
	f := newFixture(t)
	set := installInjected(f, "previous_listener_verify")
	item := prepare(t, f, samplePlan())
	// Force the forward path to fail first, then inject failure specifically at
	// restored-listener verification.
	set.failAt = "target_activate"
	set.failed = false
	set.trace = nil
	// The one-shot target failure is consumed; switch the next failure in the
	// restore hook by using a tiny wrapper.
	f.svc.Inbound = rollbackVerifyInbound{set: set}
	result, err := f.svc.Apply(context.Background(), item.OperationID, "APPLY PORT HANDOFF "+item.OperationID)
	if err == nil || result.State != StateRollbackFailed || len(f.recovery.Calls) != 1 {
		t.Fatalf("result=%#v err=%v trace=%v", result, err, set.trace)
	}
}

func TestNormalCIUsesFakeProcessExecutor(t *testing.T) {
	fake := &FakeProcessExecutor{}
	adapter := SerializedRestartAdapter{Executor: fake}
	fence := Fence{OperationID: "operation-fake", Revision: 7, InstanceID: "fake-ci"}
	if err := adapter.Restart(context.Background(), fence); err != nil {
		t.Fatal(err)
	}
	if len(fake.Calls) != 1 || fake.Calls[0] != fence {
		t.Fatalf("calls=%#v", fake.Calls)
	}
}

func TestArtifactCheckpointIsTypedDurableAndIdempotent(t *testing.T) {
	f := newFixture(t)
	item := prepare(t, f, samplePlan())
	storage, err := protectionartifacts.New(filepath.Join(t.TempDir(), ".runtime", "server-protection"))
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := ArtifactCheckpoint{Artifacts: protectionartifacts.Service{Storage: storage, Store: f.repo}, Marker: storage}
	fence := Fence{OperationID: item.OperationID, Revision: 1, InstanceID: f.manager.InstanceID()}
	if err := checkpoint.Checkpoint(context.Background(), samplePrevious(), sampleNext(), fence); err != nil {
		t.Fatal(err)
	}
	if err := checkpoint.Checkpoint(context.Background(), samplePrevious(), sampleNext(), fence); err != nil {
		t.Fatalf("idempotent checkpoint failed: %v", err)
	}
	artifact, err := f.repo.ArtifactByOperation(context.Background(), item.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := storage.VerifyRevision(artifact.Revision, artifact.ManifestSHA256)
	if err != nil || len(manifest.Files) != 2 || manifest.Files[0].Path != "resource-after.json" || manifest.Files[1].Path != "resource-before.json" {
		t.Fatalf("manifest=%#v err=%v", manifest, err)
	}
}

type rollbackVerifyInbound struct{ set *injectedAdapters }

func (a rollbackVerifyInbound) Prepare(context.Context, OwnerSnapshot, Fence) error {
	return a.set.step("inbound_prepare")
}
func (a rollbackVerifyInbound) AbortPrepare(context.Context, OwnerSnapshot, Fence) error {
	return a.set.step("inbound_abort_prepare")
}
func (a rollbackVerifyInbound) Apply(context.Context, OwnerSnapshot, Fence) (CoreMutationResult, error) {
	return CoreMutationResult{}, a.set.step("target_activate")
}
func (a rollbackVerifyInbound) Rollback(context.Context, OwnerSnapshot, OwnerSnapshot, Fence) (CoreMutationResult, error) {
	a.set.trace = append(a.set.trace, "inbound_restore")
	a.set.owners.SetCurrent(samplePrevious())
	a.set.failAt, a.set.failed = "previous_listener_verify", false
	return CoreMutationResult{}, nil
}
