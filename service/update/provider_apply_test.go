package update

import (
	"context"
	"errors"
	"testing"
	"time"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/core/registry/facts"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilityview"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

type maintenanceFixtureKey struct{}
type transitionAdmissionFixture struct {
	active bool
	ends   int
}

func (owner *transitionAdmissionFixture) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, maintenanceFixtureKey{}, owner)
}
func (owner *transitionAdmissionFixture) Active() bool { return owner.active }
func (owner *transitionAdmissionFixture) End()         { owner.active = false; owner.ends++ }

type applyConfigurationFixture struct {
	CoreConfigurationSnapshot
	admission *transitionAdmissionFixture
	input     []byte
	holds     []bool
}

func (owner *applyConfigurationFixture) CaptureProviderConfiguration(ctx context.Context) (CoreConfigurationSnapshot, error) {
	if ctx.Value(maintenanceFixtureKey{}) != owner.admission || !owner.admission.Active() {
		return CoreConfigurationSnapshot{}, ErrCoreProviderMaintenance
	}
	value := owner.CoreConfigurationSnapshot
	owner.input = []byte(`{"log":{"disabled":true}}`)
	value.Configuration = owner.input
	value.Maintenance = len(owner.holds) > 0 && owner.holds[len(owner.holds)-1]
	return value, nil
}
func (owner *applyConfigurationFixture) SetCoreMaintenance(ctx context.Context, generation string, held bool) error {
	if ctx.Value(maintenanceFixtureKey{}) != owner.admission || !owner.admission.Active() {
		return ErrCoreProviderMaintenance
	}
	owner.holds = append(owner.holds, held)
	return nil
}

type applyProviderFixture struct {
	*lifecycleProvider
	fixture              lifecycleFixture
	owner                *applyConfigurationFixture
	preflightErr         error
	rollbackAdmissionErr error
	checkpoint           bool
	startupPending       bool
	startupErr           error
}

func (provider *applyProviderFixture) PreflightProvider(ctx context.Context, operation model.UpdateOperation, verified release.Verified, artifacts []release.Artifact,
	snapshot CoreConfigurationSnapshot, persist func(string) (model.UpdateOperation, error)) (PreflightResult, error) {
	if !provider.owner.admission.Active() || ctx.Value(maintenanceFixtureKey{}) != provider.owner.admission {
		return PreflightResult{}, ErrCoreProviderMaintenance
	}
	checkpoint, err := persist(lifecycleDigest("provider-checkpoint"))
	if err != nil {
		return PreflightResult{}, err
	}
	stored, err := provider.fixture.manager.repo.operation(ctx, checkpoint.OperationID)
	if err != nil || !stored.RollbackAvailable || stored.BackupRef != checkpoint.BackupRef {
		return PreflightResult{}, ErrRecoveryRequired
	}
	provider.checkpoint = true
	if err := provider.owner.SetCoreMaintenance(ctx, snapshot.RuntimeGeneration, true); err != nil {
		return PreflightResult{}, err
	}
	return PreflightResult{RollbackAvailable: true, BackupRef: checkpoint.BackupRef}, provider.preflightErr
}
func (provider *applyProviderFixture) AdmitProviderRequest(context.Context, model.UpdateOperation, ProviderTransitionRequest) error {
	return nil
}
func (provider *applyProviderFixture) providerCheckpoint(context.Context, model.UpdateOperation) (bool, error) {
	return provider.checkpoint, nil
}

func (provider *applyProviderFixture) pendingProviderMaintenance(context.Context) (bool, error) {
	return provider.startupPending, provider.startupErr
}
func (provider *applyProviderFixture) AdmitProviderRollback(context.Context, model.UpdateOperation) error {
	return provider.rollbackAdmissionErr
}
func (provider *applyProviderFixture) VerifyActive(ctx context.Context, operation model.UpdateOperation) (bool, error) {
	if provider.verifyErr == nil {
		if err := provider.owner.SetCoreMaintenance(ctx, "", false); err != nil {
			return false, err
		}
	}
	return provider.lifecycleProvider.VerifyActive(ctx, operation)
}
func (provider *applyProviderFixture) Rollback(ctx context.Context, operation model.UpdateOperation) (bool, error) {
	if provider.rollbackErr == nil {
		if err := provider.owner.SetCoreMaintenance(ctx, "", false); err != nil {
			return false, err
		}
	}
	return provider.lifecycleProvider.Rollback(ctx, operation)
}

func newProviderApplyFixture(t *testing.T) (lifecycleFixture, *applyProviderFixture, ProviderTransitionRequest) {
	t.Helper()
	fixture, _ := providerTransitionFixture(t)
	manifest := fixture.verified.Manifest
	manifest.Version = configidentity.GetVersion()
	verified, raw, trust, err := verifyArtifactFixture(manifest, fixture.manager.now())
	if err != nil {
		t.Fatal(err)
	}
	fixture.verified, fixture.manager.fetcher, fixture.manager.trust = verified, lifecycleReleaseClient{raw: raw}, trust
	admission := &transitionAdmissionFixture{}
	owner := &applyConfigurationFixture{admission: admission}
	owner.CoreConfigurationSnapshot = CoreConfigurationSnapshot{ConfigurationDigest: semanticBytesDigest([]byte(`{"log":{"disabled":true}}`)),
		ConfigurationRevision: lifecycleDigest("provider-desired"), RuntimeGeneration: "prior-generation", RuntimeState: "RUNNING",
		Capabilities: capabilityview.Snapshot{Schema: capabilityview.Schema, ComponentProfile: "full", Provider: fixture.manager.coreProvider(),
			Facts: []capabilityview.Fact{{Fact: facts.Fact{Category: "outbounds", Type: "direct", Known: true, Registered: true, Compiled: true, SupportedByProduct: true}, RuntimeType: "direct", ContextSupported: true}}}}
	owner.CapabilityRevision = owner.Capabilities.Revision()
	provider := &applyProviderFixture{lifecycleProvider: fixture.provider, fixture: fixture, owner: owner}
	fixture.manager.provider = provider
	fixture.manager.SetCoreConfigurationOwner(owner)
	fixture.manager.beginMaintenance = func(context.Context) (providerDatabaseMaintenance, error) {
		admission.active = true
		return admission, nil
	}
	selected, err := verified.PanelArtifactsForProvider("linux", "amd64", "full", "shtorm-extended")
	if err != nil {
		t.Fatal(err)
	}
	return fixture, provider, ProviderTransitionRequest{Channel: release.ChannelMain, TargetProvider: "shtorm-extended", ExpectedSequence: verified.Manifest.Sequence,
		ExpectedManifestDigest: verified.Digest, ExpectedArtifactSetDigest: release.ArtifactSetDigest(selected), ExpectedConfigurationRevision: owner.ConfigurationRevision,
		ExpectedProviderRevision: fixture.manager.coreProvider().Revision, IdempotencyKey: "provider-maintenance-apply", Acknowledged: true}
}

func TestProviderTransitionApplyKeepsAdmissionAcrossRestartAndUsesExistingOperation(t *testing.T) {
	fixture, provider, request := newProviderApplyFixture(t)
	provider.verifyErr, provider.reconcileState = ErrRestartPending, StateVerifyingActive
	pending, err := fixture.manager.ApplyProviderTransition(t.Context(), request)
	if err != nil || State(pending.State) != StateVerifyingActive || !provider.owner.admission.Active() || provider.owner.admission.ends != 0 {
		t.Fatal("pending restart released the maintenance admission", pending.State, err)
	}
	if len(provider.owner.input) == 0 || string(provider.owner.input) != string(make([]byte, len(provider.owner.input))) {
		t.Fatal("raw configuration escaped the preflight lifetime")
	}
	provider.verifyErr = nil
	if err := fixture.manager.ReconcileStartup(t.Context()); err != nil {
		t.Fatal(err)
	}
	stored, err := fixture.manager.Operation(t.Context(), pending.OperationID)
	if err != nil || State(stored.State) != StateApplied || provider.owner.admission.Active() || provider.owner.admission.ends != 1 || provider.rollbackCalls != 0 {
		t.Fatal("runtime proof did not commit through the shared Update owner", stored.State, err)
	}
	var count int64
	if err := fixture.db.Model(&model.UpdateOperation{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("provider transition created competing operations", err)
	}
}

func TestProviderTransitionApplyFailureRestoresPriorIntentFromDurableCheckpoint(t *testing.T) {
	fixture, provider, request := newProviderApplyFixture(t)
	provider.preflightErr = errors.New("target rejects preserved field")
	operation, err := fixture.manager.ApplyProviderTransition(t.Context(), request)
	if err == nil || State(operation.State) != StateRolledBack || provider.rollbackCalls != 1 || provider.owner.admission.Active() ||
		len(provider.owner.holds) != 2 || !provider.owner.holds[0] || provider.owner.holds[1] {
		t.Fatal("failed target validation lost the checkpoint or prior hold", operation.State, err)
	}
}

func TestProviderTransitionApplyRejectsStalePreviewBeforeHoldingCore(t *testing.T) {
	fixture, provider, request := newProviderApplyFixture(t)
	request.ExpectedConfigurationRevision = lifecycleDigest("stale-desired")
	operation, err := fixture.manager.ApplyProviderTransition(t.Context(), request)
	if err == nil || State(operation.State) != StateFailed || len(provider.owner.holds) != 0 || provider.owner.admission.Active() || provider.activateCalls != 0 || provider.checkpoint {
		t.Fatal("stale preview changed maintenance or activation", operation.State, err)
	}
}

func TestProviderTransitionRuntimeProbesDoNotExtendActivationDeadline(t *testing.T) {
	fixture, provider, request := newProviderApplyFixture(t)
	provider.verifyErr, provider.reconcileState = ErrRestartPending, StateVerifyingActive
	operation, err := fixture.manager.ApplyProviderTransition(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	initial := fixture.manager.now()
	for _, elapsed := range []time.Duration{time.Minute, 4 * time.Minute, activationHealthDeadline + time.Second} {
		fixture.manager.now = func() time.Time { return initial.Add(elapsed) }
		err = fixture.manager.ReconcileStartup(t.Context())
	}
	stored, storedErr := fixture.manager.Operation(t.Context(), operation.OperationID)
	if err == nil || storedErr != nil || State(stored.State) != StateRolledBack || provider.owner.admission.Active() {
		t.Fatal("probe refreshed the activation deadline", stored.State, err, storedErr)
	}
}

func TestProviderTransitionWatchdogDrainsOnCancellation(t *testing.T) {
	fixture, _, _ := newProviderApplyFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); fixture.manager.RunMaintenanceWatchdog(ctx) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Update watchdog outlived its app context")
	}
}

func TestProviderTransitionWatchdogCancellationWhileManagerBusy(t *testing.T) {
	manager := &LifecycleManager{}
	manager.mu.Lock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		manager.mu.Unlock()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("Update watchdog did not join after cleanup")
		}
	})
	started := make(chan struct{})
	go func() {
		defer close(done)
		close(started)
		manager.RunMaintenanceWatchdog(ctx)
	}()
	<-started
	// Keep staging's manager lock held across a reconciliation tick, then
	// require shutdown to finish before that other operation releases it.
	time.Sleep(2200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Update watchdog cancellation waited for a busy manager")
	}
}

func TestProviderTransitionRollbackRejectionPreservesAnExistingHold(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "new-admission", true: "retained-admission"}[pending], func(t *testing.T) {
			fixture, provider, request := newProviderApplyFixture(t)
			if pending {
				provider.verifyErr = ErrRestartPending
			}
			operation, err := fixture.manager.ApplyProviderTransition(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			ends, holds := provider.owner.admission.ends, len(provider.owner.holds)
			provider.rollbackAdmissionErr = ErrReleaseChanged
			rejected, err := fixture.manager.Rollback(t.Context(), RevisionRequest{OperationID: operation.OperationID, ExpectedRevision: operation.Revision})
			if !errors.Is(err, ErrReleaseChanged) || rejected.Revision != operation.Revision || provider.rollbackCalls != 0 || len(provider.owner.holds) != holds ||
				provider.owner.admission.Active() != pending {
				t.Fatal("rejected rollback changed Core intent, operation or an existing admission", err)
			}
			if pending && provider.owner.admission.ends != ends || !pending && provider.owner.admission.ends != ends+1 {
				t.Fatal("rollback rejection ended another operation's admission or leaked its own")
			}
			fixture.manager.endProviderMaintenance()
		})
	}
}

func TestProviderTransitionStartupHonorsRootCheckpointWithoutDatabaseMarker(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy-root-unavailable", true: "missing-application-history"}[pending], func(t *testing.T) {
			fixture, provider, _ := newProviderApplyFixture(t)
			provider.startupPending, provider.startupErr = pending, ErrProviderUnavailable
			err := fixture.manager.RecoverProviderMaintenance(t.Context())
			if pending {
				if !errors.Is(err, ErrRecoveryRequired) || !provider.owner.admission.Active() || len(provider.owner.holds) != 1 || !provider.owner.holds[0] {
					t.Fatal("live root checkpoint was ignored after application history loss", err)
				}
				if err := fixture.manager.ReconcileStartup(t.Context()); err != nil || !provider.owner.admission.Active() {
					t.Fatal("missing operation synthesized completion or released recovery admission", err)
				}
			} else if !errors.Is(err, ErrProviderUnavailable) || provider.owner.admission.Active() || len(provider.owner.holds) != 0 {
				t.Fatal("legacy root absence acquired provider maintenance authority", err)
			}
			fixture.manager.endProviderMaintenance()
		})
	}
}
