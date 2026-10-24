//go:build linux

package updatebroker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilityview"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

type maintenanceControlFixture struct {
	readyErr, restartErr error
	restarts             int
}

func (control *maintenanceControlFixture) Ready(context.Context) error { return control.readyErr }
func (control *maintenanceControlFixture) RestartPanel(context.Context) error {
	control.restarts++
	return control.restartErr
}

type maintenanceRuntimeFixture struct {
	host     *Host
	runtime  *nativeMaintenanceRuntime
	control  *maintenanceControlFixture
	identity ReleaseIdentityV1
	previous string
	now      time.Time
}

func newMaintenanceRuntimeFixture(t *testing.T) *maintenanceRuntimeFixture {
	t.Helper()
	requireRootUpdateBrokerTest(t)
	root, err := os.MkdirTemp("/", "solovey-maintenance-runtime-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	host := NewHost()
	host.ReleaseRoot, host.StatePath, host.Manifest = filepath.Join(root, "releases"), filepath.Join(root, "state.json"), filepath.Join(root, "broker-clients.json")
	host.OwnerManifest = func(context.Context, string, string) error { return nil }
	host.Restart = func(int) { t.Error("provider maintenance used the legacy stack restart") }
	if err := os.MkdirAll(host.ReleaseRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	identity := providerReleaseFixture(t, "shtorm-extended")
	previous := providerReleaseFixture(t, "sing-box")
	previous.Sequence, previous.ManifestDigest = 18, updateDigest("previous-manifest")
	for _, value := range []ReleaseIdentityV1{previous, identity} {
		writeGenerationIdentityFixture(t, host, releaseDirectoryName(value), value)
		generation := filepath.Join(host.ReleaseRoot, releaseDirectoryName(value))
		for _, name := range []string{"solovey-ui", "solovey-privileged-broker", "solovey-ssh-proof", "solovey-broker-manifest", "solovey-owner-manifest"} {
			publicationExecutable(t, filepath.Join(generation, name), "same immutable helper bytes")
		}
	}
	oldRoot := filepath.Join(host.ReleaseRoot, releaseDirectoryName(previous))
	manifest := publicationManifest(t, filepath.Join(oldRoot, "solovey-ui"))
	if err := os.WriteFile(host.Manifest, mustJSON(manifest), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := host.verifyReleaseExecutables(oldRoot); err != nil {
		t.Fatal("fixture predecessor executables", err)
	}
	if _, err := releaseBinaryProfile(oldRoot); err != nil {
		t.Fatal("fixture predecessor profile", err)
	}
	if err := host.rewriteClientManifest(oldRoot); err != nil {
		t.Fatal("fixture predecessor client authority", err)
	}
	fixture := &maintenanceRuntimeFixture{host: host, identity: identity, previous: releaseDirectoryName(previous), now: time.Unix(1_790_000_000, 0)}
	host.Now = func() time.Time { return fixture.now }
	fixture.runtime, fixture.control = newNativeMaintenanceRuntime(host), &maintenanceControlFixture{}
	fixture.runtime.control = fixture.control
	fixture.runtime.openRunning = func() (*executableobject.Object, error) {
		return executableobject.Open(filepath.Join(oldRoot, "solovey-privileged-broker"), sharedBrokerPolicy())
	}
	state := providerCheckpointState(t, identity, providerMaintenanceFixture(t))
	state.PreparedRollbackRelease = fixture.previous
	state.ActiveRelease, state.ActiveSequence, state.ActiveDigest = releaseDirectoryName(identity), identity.Sequence, identity.ManifestDigest
	state.MaxActivatedSequence, state.MaxActivatedDigest = identity.Sequence, identity.ManifestDigest
	state.UpdatedAt = fixture.now.Unix()
	proof := &state.ProviderMaintenance
	capabilities := providerCapabilityFixture(t, "shtorm-extended", "full")
	difference, err := capabilityview.Compare(providerCapabilityFixture(t, "sing-box", "full"), capabilities)
	if err != nil {
		t.Fatal(err)
	}
	proof.Phase, proof.ExecutableDigest, proof.ReportDigest = "ACTIVATED", updateDigest("executable"), updateDigest("report")
	proof.TargetCapabilities, proof.CapabilityRevision = string(mustJSON(capabilities)), capabilities.Revision()
	proof.PreviousReportDigest, proof.CapabilityDifference = updateDigest("previous-report"), string(mustJSON(difference))
	if err := host.saveState(state); err != nil || host.activateLink(state.ActiveRelease) != nil {
		t.Fatal("fixture activation failed", err)
	}
	return fixture
}

func (fixture *maintenanceRuntimeFixture) load(t *testing.T) diskState {
	t.Helper()
	state, err := fixture.host.loadState()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestMaintenanceRuntimeDeadlineRecoversWithoutAnyPanelPeer(t *testing.T) {
	fixture := newMaintenanceRuntimeFixture(t)
	activated := fixture.load(t)
	if err := fixture.runtime.reconcile(t.Context()); err != nil || fixture.control.restarts != 0 {
		t.Fatal("reply grace was not retained", err)
	}
	fixture.now = fixture.now.Add(providerRestartReplyGrace)
	if err := fixture.runtime.reconcile(t.Context()); err != nil || fixture.control.restarts != 1 {
		t.Fatal("candidate Panel was not restarted", err)
	}
	if err := fixture.runtime.reconcile(t.Context()); err != nil || fixture.control.restarts != 1 || fixture.load(t) != activated {
		t.Fatal("repeated observation restarted the Panel or extended its deadline", err)
	}
	fixture.now = time.Unix(activated.UpdatedAt, 0).Add(ProviderActivationHealthDeadline)
	if err := fixture.runtime.reconcile(t.Context()); err != nil {
		t.Fatal("Root could not recover a candidate that never produced a Panel peer", err)
	}
	rolledBack := fixture.load(t)
	if rolledBack.ProviderMaintenance.Phase != "ROLLED_BACK" || rolledBack.ActiveRelease != fixture.previous ||
		rolledBack.VerifiedSequence != 0 || rolledBack.VerifiedDigest != "" || rolledBack.MaxActivatedSequence != activated.MaxActivatedSequence ||
		rolledBack.ProviderMaintenance.Input != activated.ProviderMaintenance.Input || !rolledBack.hasBoundPreparedRollback() {
		t.Fatal("deadline rollback lost checkpoint/high-water authority or fabricated runtime proof")
	}
	fixture.now = fixture.now.Add(providerRestartReplyGrace)
	if err := fixture.runtime.reconcile(t.Context()); err != nil || fixture.control.restarts != 2 {
		t.Fatal("previous Panel was not restarted through the same owner", err)
	}
	if target, err := os.Readlink(filepath.Join(fixture.host.ReleaseRoot, "current")); err != nil || filepath.Base(target) != fixture.previous {
		t.Fatal("rollback did not restore the active release link", err)
	}
}

func TestMaintenanceRuntimeRestartFailureUsesExistingCheckpoint(t *testing.T) {
	fixture := newMaintenanceRuntimeFixture(t)
	fixture.control.restartErr = errors.New("service control failure")
	fixture.now = fixture.now.Add(providerRestartReplyGrace)
	if err := fixture.runtime.reconcile(t.Context()); err != nil || fixture.load(t).ProviderMaintenance.Phase != "ROLLED_BACK" {
		t.Fatal("restart failure did not roll back the existing checkpoint", err)
	}
	fixture.now = fixture.now.Add(providerRestartReplyGrace)
	if err := fixture.runtime.reconcile(t.Context()); err == nil || fixture.control.restarts != 2 {
		t.Fatal("failed predecessor restart fabricated recovery success", err)
	}
	if err := fixture.runtime.reconcile(t.Context()); err != nil || fixture.control.restarts != 2 {
		t.Fatal("failed service control was retried without its bounded delay", err)
	}
	fixture.control.restartErr = nil
	fixture.now = fixture.now.Add(releaseControlTimeout)
	if err := fixture.runtime.reconcile(t.Context()); err != nil || fixture.control.restarts != 3 || fixture.load(t).VerifiedSequence != 0 {
		t.Fatal("repaired service control could not resume the pending rollback", err)
	}
}

func TestMaintenanceRuntimeSharedBrokerRequiresActualTrustedBytes(t *testing.T) {
	fixture := newMaintenanceRuntimeFixture(t)
	root := filepath.Join(fixture.host.ReleaseRoot, releaseDirectoryName(fixture.identity))
	if err := fixture.runtime.admit(t.Context(), root); err != nil {
		t.Fatal("identical trusted broker was rejected", err)
	}
	path := filepath.Join(root, "solovey-privileged-broker")
	publicationExecutable(t, path, "different helper with the same Panel version")
	if fixture.runtime.admit(t.Context(), root) == nil || fixture.host.runningRelease(filepath.Base(root), broker.PeerIdentity{Executable: filepath.Join(root, "solovey-ui")}) {
		t.Fatal("different running broker bytes were accepted by name/version")
	}
	publicationExecutable(t, path, "same immutable helper bytes")
	if err := os.Chmod(path, 0o777); err != nil {
		t.Fatal(err)
	}
	if fixture.runtime.admit(t.Context(), root) == nil {
		t.Fatal("writable broker bytes were accepted")
	}
	fixture.control.readyErr = errors.New("fixed service unavailable")
	publicationExecutable(t, path, "same immutable helper bytes")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if fixture.runtime.admit(t.Context(), root) == nil {
		t.Fatal("unavailable fixed service control was admitted")
	}
}

func TestMaintenanceRuntimeBusyOwnerAndCancellationJoin(t *testing.T) {
	fixture := newMaintenanceRuntimeFixture(t)
	fixture.runtime.mu.Lock()
	defer fixture.runtime.mu.Unlock()
	if err := fixture.runtime.reconcile(t.Context()); err != nil {
		t.Fatal("watcher did not skip a busy update owner", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	called := false
	handler := fixture.runtime.handler(func(context.Context, broker.Request, broker.PeerIdentity) (any, error) {
		called = true
		return nil, nil
	})
	done := make(chan error, 1)
	go func() { _, err := handler(ctx, broker.Request{}, broker.PeerIdentity{}); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !brokerErrorCode(err, broker.CodeDeadline) || called {
			t.Fatal("canceled owner admission executed a mutation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled owner admission did not join while the owner remained busy")
	}
}

func TestMaintenanceRuntimePruningRetainsExecutingBrokerGeneration(t *testing.T) {
	fixture := newMaintenanceRuntimeFixture(t)
	garbage := "00000000000000000007-aaaaaaaaaaaaaaaa"
	if err := os.Mkdir(filepath.Join(fixture.host.ReleaseRoot, garbage), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture.host.pruneReleases(releaseDirectoryName(fixture.identity))
	if _, err := os.Stat(filepath.Join(fixture.host.ReleaseRoot, fixture.previous)); err != nil {
		t.Fatal("executing broker generation was pruned", err)
	}
	if _, err := os.Stat(filepath.Join(fixture.host.ReleaseRoot, garbage)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unreferenced generation was retained", err)
	}
}

func TestMaintenanceRuntimeCannotInventOrRefreshDeadlineAuthority(t *testing.T) {
	fixture := newMaintenanceRuntimeFixture(t)
	activated := fixture.load(t)
	fixture.now = fixture.now.Add(-time.Second)
	if err := fixture.runtime.reconcile(t.Context()); err == nil || fixture.load(t) != activated || fixture.control.restarts != 0 {
		t.Fatal("clock reversal manufactured a fresh activation deadline", err)
	}
	fixture.now = fixture.now.Add(ProviderActivationHealthDeadline * 2)
	committed := activated
	committed.ProviderMaintenance.Phase = "COMMITTED"
	committed.promotePreparedRollback()
	committed.VerifiedSequence, committed.VerifiedDigest = committed.ActiveSequence, committed.ActiveDigest
	if err := fixture.host.saveState(committed); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runtime.reconcile(t.Context()); err != nil || fixture.load(t) != committed || fixture.control.restarts != 0 {
		t.Fatal("completed maintenance was restarted or rolled back by a stale deadline", err)
	}
}

func TestNativeRuntimeRegistrationBindsRecoveryAndCannotBeReused(t *testing.T) {
	runtime := NewRuntime()
	registry := broker.NewRegistry()
	if err := runtime.RegisterHandlers(registry, ModeNativeSelfManaged); err != nil || runtime.reconcile == nil || len(registry.Verbs(broker.RolePanel)) != 7 {
		t.Fatal("native handler composition omitted its recovery owner", err)
	}
	if runtime.RegisterHandlers(broker.NewRegistry(), ModePackageManaged) == nil {
		t.Fatal("a bound native recovery owner was replaced")
	}
}
