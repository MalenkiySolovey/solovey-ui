//go:build linux

package updatebroker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilityview"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestOrdinaryUpdateStateRequiresItsOwnCheckpointAndNoMaintenanceClaims(t *testing.T) {
	identity := providerReleaseFixture(t, "shtorm-extended")
	state := providerCheckpointState(t, identity, providerMaintenanceFixture(t))
	state.ProviderMaintenance = providerMaintenanceState{}
	state.OrdinaryUpdate = ordinaryUpdateBinding(state.PreparedRollbackOperationID, identity, identity.CoreProvider)
	if err := validateDiskState(state); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*diskState){
		func(s *diskState) {
			s.ProviderMaintenance = providerCheckpointState(t, identity, providerMaintenanceFixture(t)).ProviderMaintenance
		},
		func(s *diskState) { s.PreparedRollbackOperationID = "update-operation:other" },
		func(s *diskState) { s.OrdinaryUpdate.Sequence++ },
		func(s *diskState) { s.ActiveRelease = "00000000000000000017-2222222222222222" },
	} {
		changed := state
		change(&changed)
		if validateDiskState(changed) == nil {
			t.Fatal("mixed ordinary/maintenance/rollback authority accepted")
		}
	}
	host := NewHost()
	host.ReleaseRoot = t.TempDir()
	state.ActiveRelease, state.ActiveSequence, state.ActiveDigest = releaseDirectoryName(identity), identity.Sequence, identity.ManifestDigest
	state.clearPreparedRollback()
	writeGenerationIdentityFixture(t, host, state.ActiveRelease, identity)
	state.OrdinaryUpdate = OrdinaryUpdateV1{}
	if host.legacyAutoVerificationAllowed(state) {
		t.Fatal("lost provider binding became implicit maintenance authority")
	}
	if err := host.admitProviderActivation(broker.Request{OperationID: "update-operation:ordinary"}, ActivateRequestV1{}, identity, state, broker.PeerIdentity{}); err == nil {
		t.Fatal("missing binding authorized a provider activation")
	}
}

func TestOrdinaryPreparedCheckpointRetainsAndCommitsPriorMaintenanceAuthority(t *testing.T) {
	fixture := newMaintenanceRuntimeFixture(t)
	state := fixture.load(t)
	state.promotePreparedRollback()
	state.VerifiedSequence, state.VerifiedDigest = state.ActiveSequence, state.ActiveDigest
	state.ProviderMaintenance.Phase = "COMMITTED"
	prior := state.ProviderMaintenance
	if err := validateDiskState(state); err != nil {
		t.Fatal(err)
	}
	target := fixture.identity
	target.Sequence++
	target.ManifestDigest = updateDigest("ordinary-next-envelope")
	op := "update-operation:ordinary-next"
	state.PreparedRollbackRelease, state.PreparedRollbackSequence, state.PreparedRollbackDigest = state.ActiveRelease, state.ActiveSequence, state.ActiveDigest
	state.PreparedRollbackOperationID, state.PreparedRollbackManifestDigest, state.PreparedRollbackRef = op, target.ManifestDigest, SemanticReference("rollback", op, target.ManifestDigest)
	state.PreparedPriorMaintenance = prior
	state.ProviderMaintenance = providerMaintenanceState{}
	state.OrdinaryUpdate = ordinaryUpdateBinding(op, target, fixture.identity.CoreProvider)
	if err := fixture.host.saveState(state); err != nil {
		t.Fatal("prior live rollback could not accompany prepared ordinary update", err)
	}
	if reopened, err := fixture.host.loadState(); err != nil || reopened != state {
		t.Fatal("restart lost the prior authority", err)
	}
	for _, change := range []func(*diskState){
		func(s *diskState) { s.PreparedPriorMaintenance.Phase = "ACTIVATED" },
		func(s *diskState) { s.OrdinaryUpdate = OrdinaryUpdateV1{} },
		func(s *diskState) { s.RollbackOperationID = "update-operation:other" },
		func(s *diskState) {
			s.PreparedPriorMaintenance.TargetProvider = providerReleaseFixture(t, "sing-box").CoreProvider
		},
	} {
		changed := state
		change(&changed)
		if validateDiskState(changed) == nil {
			t.Fatal("unrelated, pending or orphaned previous provider authority accepted")
		}
	}
	state.ActiveRelease, state.ActiveSequence, state.ActiveDigest = releaseDirectoryName(target), target.Sequence, target.ManifestDigest
	state.VerifiedSequence, state.VerifiedDigest = state.ActiveSequence, state.ActiveDigest
	state.raiseHighWater(state.ActiveSequence, state.ActiveDigest)
	state.promotePreparedRollback()
	if validateDiskState(state) != nil || state.PreparedPriorMaintenance != (providerMaintenanceState{}) || state.RollbackOperationID != op {
		t.Fatal("ordinary commit did not consume only its prepared predecessor")
	}
}

// Uses the same sealed candidate ELF as native release qualification. No test
// command, path, service or provider claim is admitted as production authority.
func TestOrdinaryUpdateActualCandidatePrepareActivateVerifyRollback(t *testing.T) {
	testOrdinaryUpdateActualLifecycle(t, false, false)
}

func TestOrdinaryUpdateActualCandidateInstallerCheckpointRollback(t *testing.T) {
	testOrdinaryUpdateActualLifecycle(t, true, false)
}

func TestOrdinaryUpdateActualCandidateRetainsPriorMaintenanceRollbackUntilCommit(t *testing.T) {
	testOrdinaryUpdateActualLifecycle(t, false, true)
}

func testOrdinaryUpdateActualLifecycle(t *testing.T, installer, priorMaintenance bool) {
	t.Helper()
	binary := os.Getenv("SOLOVEY_PROVIDER_TEST_BINARY")
	if binary == "" {
		t.Skip("actual candidate supplied during corrective release qualification")
	}
	requireRootUpdateBrokerTest(t)
	id, profile := os.Getenv("SOLOVEY_PROVIDER_TEST_ID"), os.Getenv("SOLOVEY_PROVIDER_TEST_PROFILE")
	identity := providerReleaseFixture(t, id)
	identity.BinaryProfile, identity.Artifacts[0].Role = profile, "panel-"+profile
	archive := []byte("already published root-owned candidate fixture")
	identity.Artifacts[0].Size, identity.Artifacts[0].SHA256 = int64(len(archive)), broker.Digest(archive)
	identity.ArtifactSetDigest = artifactSetDigest(identity.Artifacts)
	root, err := os.MkdirTemp("/", "solovey-ordinary-update-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	host := NewHost()
	host.ReleaseRoot, host.InboxRoot, host.StatePath, host.Manifest = filepath.Join(root, "releases"), filepath.Join(root, "inbox"), filepath.Join(root, "state.json"), filepath.Join(root, "broker-clients.json")
	host.OwnerManifest, host.Restart = func(context.Context, string, string) error { return nil }, nil
	if err := os.Mkdir(host.ReleaseRoot, 0755); err != nil {
		t.Fatal(err)
	}
	previous := identity
	previous.Sequence, previous.ManifestDigest = 18, updateDigest("ordinary-previous")
	for _, value := range []ReleaseIdentityV1{previous, identity} {
		writeGenerationIdentityFixture(t, host, releaseDirectoryName(value), value)
		generation := filepath.Join(host.ReleaseRoot, releaseDirectoryName(value))
		providerActualCopy(t, binary, filepath.Join(generation, "solovey-ui"))
		for _, name := range []string{"solovey-privileged-broker", "solovey-ssh-proof", "solovey-broker-manifest"} {
			publicationExecutable(t, filepath.Join(generation, name), "same fixed helper")
		}
		if profile == "full" {
			publicationExecutable(t, filepath.Join(generation, "solovey-owner-manifest"), "fixed owner")
		}
	}
	previousName := releaseDirectoryName(previous)
	if installer {
		previousName = "installer-" + updateDigest("ordinary-installer")[:16]
		if err := os.Rename(filepath.Join(host.ReleaseRoot, releaseDirectoryName(previous)), filepath.Join(host.ReleaseRoot, previousName)); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(host.ReleaseRoot, previousName, "release-identity.json")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(host.ReleaseRoot, previousName, "BUILD_INFO.txt"), []byte("profile="+profile+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	oldRoot := filepath.Join(host.ReleaseRoot, previousName)
	manifest := publicationManifest(t, filepath.Join(oldRoot, "solovey-ui"))
	if err := os.WriteFile(host.Manifest, mustJSON(manifest), 0640); err != nil {
		t.Fatal(err)
	}
	if err := host.activateLink(previousName); err != nil {
		t.Fatal(err)
	}
	state := diskState{ActiveRelease: releaseDirectoryName(previous), ActiveSequence: previous.Sequence, ActiveDigest: previous.ManifestDigest, VerifiedSequence: previous.Sequence, VerifiedDigest: previous.ManifestDigest, MaxActivatedSequence: previous.Sequence, MaxActivatedDigest: previous.ManifestDigest}
	var prior providerMaintenanceState
	if priorMaintenance {
		input := providerMaintenanceFixture(t)
		other := "sing-box"
		if id == other {
			other = "shtorm-extended"
		}
		previousCaps, targetCaps := providerCapabilityFixture(t, other, profile), providerCapabilityFixture(t, id, profile)
		input.PreviousProvider, input.PreviousCapabilityRevision = previousCaps.Provider.Source, previousCaps.Revision()
		difference, err := capabilityview.Compare(previousCaps, targetCaps)
		if err != nil {
			t.Fatal(err)
		}
		prior = providerCheckpointState(t, previous, input).ProviderMaintenance
		prior.Phase, prior.ExecutableDigest, prior.ReportDigest = "COMMITTED", updateDigest("prior-executable"), updateDigest("prior-report")
		prior.TargetCapabilities, prior.CapabilityRevision = string(mustJSON(targetCaps)), targetCaps.Revision()
		prior.PreviousReportDigest, prior.CapabilityDifference = updateDigest("prior-previous-report"), string(mustJSON(difference))
		state.ProviderMaintenance = prior
		state.RollbackRelease, state.RollbackSequence, state.RollbackDigest = "00000000000000000017-1111111111111111", 17, updateDigest("prior-predecessor")
		state.RollbackOperationID, state.RollbackManifestDigest, state.RollbackRef = prior.OperationID, prior.ManifestDigest, SemanticReference("rollback", prior.OperationID, prior.ManifestDigest)
	}
	if !installer {
		if err := host.saveState(state); err != nil {
			t.Fatal(err)
		}
	}
	peerFor := func(name string) broker.PeerIdentity {
		m := publicationManifest(t, filepath.Join(host.ReleaseRoot, name, "solovey-ui"))
		e := m.Clients[0]
		info, err := os.Stat(e.Executable)
		if err != nil {
			t.Fatal(err)
		}
		return broker.PeerIdentity{PID: os.Getpid(), UID: 65534, GID: 65534, Executable: e.Executable, ExecutableDigest: e.ExecutableDigest, Device: e.Device, Inode: e.Inode, ExecutableSize: info.Size()}
	}
	peer := peerFor(previousName)
	for _, mutate := range []func(*ReleaseIdentityV1){
		func(v *ReleaseIdentityV1) {
			v.Artifacts[0].Arch = "arm64"
			if identity.Artifacts[0].Arch == "arm64" {
				v.Artifacts[0].Arch = "amd64"
			}
		},
		func(v *ReleaseIdentityV1) {
			v.BinaryProfile = "core"
			if profile == "core" {
				v.BinaryProfile = "full"
			}
		},
	} {
		wrong := identity
		wrong.Artifacts = append([]ArtifactIdentityV1(nil), identity.Artifacts...)
		mutate(&wrong)
		if verifyOrdinaryProviderPeer(peer, previous.CoreProvider, wrong) == nil {
			t.Fatal("ordinary source peer admitted another architecture/profile")
		}
	}
	runtime := newNativeMaintenanceRuntime(host)
	runtime.openRunning = func() (*executableobject.Object, error) {
		return executableobject.Open(filepath.Join(oldRoot, "solovey-privileged-broker"), sharedBrokerPolicy())
	}
	op := "update-operation:ordinary-candidate"
	inbox, err := host.operationInbox(op, identity, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, identity.Artifacts[0].Role+"-"+identity.Artifacts[0].Name), archive, 0600); err != nil {
		t.Fatal(err)
	}
	request := PrepareRequestV1{Release: identity, InstalledProvider: previous.CoreProvider, ExpectedBrokerCapability: broker.CapabilityRevision, ExpectedManagementRevision: identity.DeploymentRevision}
	envelope := func(payload any) broker.Request {
		return broker.Request{OperationID: op, Payload: mustJSON(payload), Expected: broker.Revisions{Configuration: identity.DeploymentRevision}}
	}
	wrong := request
	wrong.Release.CoreProvider.ModuleSum = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	if _, err := host.prepare(t.Context(), envelope(wrong), peer); err == nil {
		t.Fatal("ordinary preflight accepted another engine source")
	}
	wrong = request
	wrong.InstalledProvider.ModuleSum = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	if _, err := host.prepare(t.Context(), envelope(wrong), peer); err == nil {
		t.Fatal("ordinary preflight accepted a forged installed source")
	}
	value, err := host.prepare(t.Context(), envelope(request), peer)
	if err != nil {
		t.Fatal("ordinary preflight failed", err)
	}
	checkpoint := value.(PrepareResultV1)
	state, err = host.loadState()
	if err != nil || !state.OrdinaryUpdate.Matches(op, identity) || state.ProviderMaintenance != (providerMaintenanceState{}) {
		t.Fatal("ordinary preparation lost its independent source binding", err)
	}
	if state.PreparedPriorMaintenance != prior {
		t.Fatal("ordinary prepare revoked live provider rollback before commit")
	}
	activation := ActivateRequestV1{Release: identity, PreparedRef: checkpoint.PreparedRef, RollbackRef: checkpoint.RollbackRef, ExpectedMode: "native"}
	activation.MaintenanceHeld = true
	if _, err := host.activate(t.Context(), envelope(activation), peer); err == nil {
		t.Fatal("ordinary update acquired maintenance authority")
	}
	activation.MaintenanceHeld = false
	if _, err := host.activate(t.Context(), envelope(activation), peer); err != nil {
		t.Fatal("ordinary activation failed", err)
	}
	verification := VerifyRequestV1{Release: identity, PreparedRef: checkpoint.PreparedRef, RollbackRef: checkpoint.RollbackRef}
	if !priorMaintenance {
		value, err = host.verify(t.Context(), envelope(verification), peerFor(releaseDirectoryName(identity)))
		if err != nil || !value.(VerifyResultV1).Verified {
			t.Fatal("ordinary runtime could not verify its unchanged provider", err)
		}
	}
	if _, err := host.rollback(t.Context(), envelope(RollbackRequestV1{Release: identity, RollbackRef: checkpoint.RollbackRef, ReasonCode: "qualification"}), peerFor(releaseDirectoryName(identity))); err != nil {
		t.Fatal("ordinary rollback failed", err)
	}
	state, err = host.loadState()
	if err != nil || state.ActiveRelease != previousName || state.MaxActivatedSequence != identity.Sequence || !priorMaintenance && !host.legacyAutoVerificationAllowed(state) {
		t.Fatal("rollback lost provider or anti-rollback evidence", err)
	}
	if priorMaintenance {
		if state.ProviderMaintenance != prior || !state.rollbackMatches(prior.OperationID, prior.ManifestDigest, SemanticReference("rollback", prior.OperationID, prior.ManifestDigest)) || state.OrdinaryUpdate != (OrdinaryUpdateV1{}) {
			t.Fatal("uncommitted ordinary update consumed prior provider rollback")
		}
		return
	}
	if state.ProviderMaintenance != (providerMaintenanceState{}) || !installer && state.OrdinaryUpdate.CoreProvider != previous.CoreProvider {
		t.Fatal("rollback confused ordinary and privileged authority")
	}
	if installer && (state.ActiveSequence != 0 || state.ActiveDigest != "" || state.OrdinaryUpdate != (OrdinaryUpdateV1{})) {
		t.Fatal("installer rollback invented signed source authority or left a new ABI field for the predecessor")
	}
}
