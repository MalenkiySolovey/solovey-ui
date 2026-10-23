//go:build linux

package updatebroker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	providerfixture "github.com/MalenkiySolovey/solovey-ui/testsupport/coreprovider"
)

func providerCheckpointState(t *testing.T, identity ReleaseIdentityV1, input ProviderMaintenanceV1) diskState {
	t.Helper()
	operationID := "update-operation:provider-maintenance"
	return diskState{ActiveRelease: "00000000000000000018-1111111111111111", ActiveSequence: 18, ActiveDigest: updateDigest("previous-manifest"),
		MaxActivatedSequence: 18, MaxActivatedDigest: updateDigest("previous-manifest"),
		PreparedRollbackRelease: "00000000000000000018-1111111111111111", PreparedRollbackSequence: 18, PreparedRollbackDigest: updateDigest("previous-manifest"),
		PreparedRollbackOperationID: operationID, PreparedRollbackManifestDigest: identity.ManifestDigest, PreparedRollbackRef: semanticRef("rollback", operationID, identity.ManifestDigest),
		ProviderMaintenance: providerMaintenanceState{OperationID: operationID, ManifestDigest: identity.ManifestDigest, ArtifactSetDigest: identity.ArtifactSetDigest,
			ReleaseDigest: broker.Digest(mustJSON(identity)), TargetRelease: releaseDirectoryName(identity), TargetSequence: identity.Sequence,
			TargetProvider: identity.CoreProvider, Input: input, Phase: "CHECKPOINTED"}}
}

func TestProviderMaintenanceEvidencePublicationReopensOnlyOldOrNewBoundState(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	host := NewHost()
	host.StatePath = filepath.Join(t.TempDir(), "update-state.json")
	old := providerCheckpointState(t, providerReleaseFixture(t, "shtorm-extended"), providerMaintenanceFixture(t))
	next := old
	next.ProviderMaintenance.Phase = "VALIDATED"
	next.ProviderMaintenance.ExecutableDigest, next.ProviderMaintenance.ReportDigest = updateDigest("target-executable"), updateDigest("exact-check-report")
	fault := errors.New("provider evidence publication fault")
	for _, at := range []string{"create", "write", "sync", "close", "rename", "sync-dir"} {
		t.Run(at, func(t *testing.T) {
			host.atomicOps = productionUpdateAtomicOps
			if err := host.saveState(old); err != nil {
				t.Fatal(err)
			}
			host.atomicOps = publicationFaultAtomicOps(at, fault)
			if err := host.saveState(next); !errors.Is(err, fault) {
				t.Fatal("publication fault was not reported")
			}
			restarted := NewHost()
			restarted.StatePath = host.StatePath
			got, err := restarted.loadState()
			want := old
			if at == "sync-dir" {
				want = next
			}
			if err != nil || got != want {
				t.Fatal("reopened evidence mixed checkpoint or validation authority", err)
			}
		})
	}
	if bytes.Contains(mustJSON(diskState{}), []byte("providerMaintenance")) {
		t.Fatal("legacy empty disk authority wire changed")
	}
	for _, change := range []func(*diskState){
		func(s *diskState) { s.ProviderMaintenance.Input.BackupRef = "" },
		func(s *diskState) { s.ProviderMaintenance.ArtifactSetDigest = updateDigest("other-selection") },
		func(s *diskState) { s.ProviderMaintenance.OperationID = "update-operation:other" },
		func(s *diskState) { s.ProviderMaintenance.Phase = "COMMITTED" },
		func(s *diskState) { s.ProviderMaintenance.ReportDigest = updateDigest("unvalidated") },
	} {
		invalid := old
		change(&invalid)
		if validateDiskState(invalid) == nil {
			t.Fatal("mixed or prematurely committed provider evidence was accepted")
		}
	}
}

func TestProviderMaintenanceObservationCannotPromoteSameManifestCheckpoint(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	self, err := os.Readlink("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	host := NewHost()
	host.ReleaseRoot = filepath.Dir(filepath.Dir(self))
	host.StatePath, host.InboxRoot = filepath.Join(t.TempDir(), "state.json"), t.TempDir()
	identity := providerReleaseFixture(t, "shtorm-extended")
	state := providerCheckpointState(t, identity, providerMaintenanceFixture(t))
	state.ActiveRelease, state.PreparedRollbackRelease = filepath.Base(filepath.Dir(self)), filepath.Base(filepath.Dir(self))
	state.ActiveSequence, state.PreparedRollbackSequence, state.MaxActivatedSequence = identity.Sequence, identity.Sequence, identity.Sequence
	state.ActiveDigest, state.PreparedRollbackDigest, state.MaxActivatedDigest = identity.ManifestDigest, identity.ManifestDigest, identity.ManifestDigest
	if err := host.saveState(state); err != nil {
		t.Fatal(err)
	}
	if _, err := host.observe(t.Context(), broker.Request{}, broker.PeerIdentity{PID: os.Getpid(), Executable: self}); err != nil {
		t.Fatal(err)
	}
	got, err := host.loadState()
	if err != nil || got != state || !got.hasBoundPreparedRollback() || got.VerifiedSequence != 0 {
		t.Fatal("running old provider consumed a same-envelope transition checkpoint", err)
	}
	projection := got.ProviderMaintenance.observation()
	if projection == nil || ValidateProviderMaintenanceObservation(*projection) != nil || projection.Input != state.ProviderMaintenance.Input || projection.TargetProvider != identity.CoreProvider {
		t.Fatal("restart observation lost authenticated owner evidence")
	}
	projection.Input.BackupRef = updateDigest("different-backup")
	if ValidateProviderMaintenanceObservation(*projection) == nil || got.ProviderMaintenance.Input.BackupRef != state.ProviderMaintenance.Input.BackupRef {
		t.Fatal("modified or aliased observation retained authority")
	}
	raw := mustJSON(state)
	ambiguous := bytes.Replace(raw, []byte(`"phase":"CHECKPOINTED"`), []byte(`"phase":"VALIDATED","phase":"CHECKPOINTED"`), 1)
	var reopened diskState
	if decodeStrict(ambiguous, &reopened) == nil {
		t.Fatal("ambiguous persisted maintenance evidence was accepted")
	}
}

func TestProviderMaintenanceRuntimeProofRequiresRestoredIntentAndCurrentHealth(t *testing.T) {
	proof := providerCheckpointState(t, providerReleaseFixture(t, "shtorm-extended"), providerMaintenanceFixture(t)).ProviderMaintenance
	health := ProviderHealthV1{ConfigurationDigest: proof.Input.ConfigurationDigest, ConfigurationRevision: proof.Input.ConfigurationRevision,
		RuntimeState: "RUNNING", RuntimeGeneration: "generation-current", CapabilityRevision: updateDigest("observed-capability")}
	if !providerHealthMatches(proof, &health) {
		t.Fatal("exact running health was not accepted")
	}
	for _, change := range []func(*ProviderHealthV1){
		func(h *ProviderHealthV1) { h.ConfigurationDigest = updateDigest("other") },
		func(h *ProviderHealthV1) { h.ConfigurationRevision = updateDigest("other") },
		func(h *ProviderHealthV1) { h.Maintenance = true },
		func(h *ProviderHealthV1) { h.RuntimeState = "UNKNOWN" },
		func(h *ProviderHealthV1) { h.RuntimeGeneration = "" },
		func(h *ProviderHealthV1) { h.CapabilityRevision = "" },
	} {
		changed := health
		change(&changed)
		if providerHealthMatches(proof, &changed) {
			t.Fatal("stale, stopped or unknown runtime proof was accepted")
		}
	}
	proof.Input.PreviousMaintenance = true
	health.Maintenance, health.RuntimeState, health.RuntimeGeneration = true, "MAINTENANCE", ""
	if !providerHealthMatches(proof, &health) || providerHealthMatches(proof, nil) {
		t.Fatal("pre-existing operator hold was not preserved")
	}
}

func providerActualCopy(t *testing.T, binary, destination string) {
	t.Helper()
	input, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		_ = input.Close()
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, input)
	outErr, inErr := output.Close(), input.Close()
	if copyErr != nil || outErr != nil || inErr != nil || os.Chmod(destination, 0o755) != nil {
		t.Fatal("actual executable fixture copy failed")
	}
}

func TestProviderMaintenanceActualCandidateCheckpointCheckActivateRollback(t *testing.T) {
	binary := os.Getenv("SOLOVEY_PROVIDER_TEST_BINARY")
	if binary == "" {
		t.Skip("actual candidate maintenance probe is supplied during provider qualification")
	}
	requireRootUpdateBrokerTest(t)
	id, profile := os.Getenv("SOLOVEY_PROVIDER_TEST_ID"), os.Getenv("SOLOVEY_PROVIDER_TEST_PROFILE")
	if profile != "full" && profile != "core" {
		t.Fatal("explicit provider/profile required")
	}
	previousBinary, previousID := os.Getenv("SOLOVEY_PROVIDER_TEST_PREVIOUS_BINARY"), os.Getenv("SOLOVEY_PROVIDER_TEST_PREVIOUS_ID")
	if previousBinary == "" {
		previousBinary, previousID = binary, id
	}
	identity := providerReleaseFixture(t, id)
	identity.BinaryProfile, identity.Artifacts[0].Role = profile, "panel-"+profile
	archiveBytes := []byte("fixture for an already published, root-owned generation")
	identity.Artifacts[0].Size, identity.Artifacts[0].SHA256 = int64(len(archiveBytes)), broker.Digest(archiveBytes)
	identity.ArtifactSetDigest = artifactSetDigest(identity.Artifacts)
	root, err := os.MkdirTemp("/", "solovey-provider-maintenance-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	host := NewHost()
	host.ReleaseRoot, host.InboxRoot, host.StatePath, host.Manifest = filepath.Join(root, "releases"), filepath.Join(root, "inbox"), filepath.Join(root, "state.json"), filepath.Join(root, "broker-clients.json")
	host.OwnerManifest, host.Restart = func(context.Context, string, string) error { return nil }, nil
	if err := os.Mkdir(host.ReleaseRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	previous := identity
	previous.Sequence, previous.ManifestDigest, previous.ReleaseID = 18, updateDigest("previous-manifest"), "previous-release"
	if previousID != id { // Exercise an explicit same-envelope flavor transition.
		previous.Sequence, previous.ManifestDigest = identity.Sequence, identity.ManifestDigest
	}
	previous.CoreProvider, previous.Artifacts = release.CoreProviderSource{}, append([]ArtifactIdentityV1(nil), identity.Artifacts...)
	previous.Artifacts[0].CoreProvider = ""
	previous.ArtifactSetDigest = artifactSetDigest(previous.Artifacts)
	for _, item := range []struct {
		identity ReleaseIdentityV1
		binary   string
	}{{previous, previousBinary}, {identity, binary}} {
		writeGenerationIdentityFixture(t, host, releaseDirectoryName(item.identity), item.identity)
		generation := filepath.Join(host.ReleaseRoot, releaseDirectoryName(item.identity))
		providerActualCopy(t, item.binary, filepath.Join(generation, "solovey-ui"))
		for _, name := range []string{"solovey-privileged-broker", "solovey-ssh-proof", "solovey-broker-manifest"} {
			publicationExecutable(t, filepath.Join(generation, name), "maintenance fixture")
		}
		if profile == "full" {
			publicationExecutable(t, filepath.Join(generation, "solovey-owner-manifest"), "maintenance fixture")
		}
	}
	oldRoot := filepath.Join(host.ReleaseRoot, releaseDirectoryName(previous))
	oldPanel := filepath.Join(oldRoot, "solovey-ui")
	manifest := publicationManifest(t, oldPanel)
	if err := os.WriteFile(host.Manifest, mustJSON(manifest), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := host.activateLink(releaseDirectoryName(previous)); err != nil {
		t.Fatal(err)
	}
	state := diskState{ActiveRelease: releaseDirectoryName(previous), ActiveSequence: previous.Sequence, ActiveDigest: previous.ManifestDigest,
		VerifiedSequence: previous.Sequence, VerifiedDigest: previous.ManifestDigest, MaxActivatedSequence: previous.Sequence, MaxActivatedDigest: previous.ManifestDigest}
	if err := host.saveState(state); err != nil {
		t.Fatal(err)
	}
	input := providerMaintenanceFixture(t)
	raw := []byte(`{"log":{"disabled":true},"outbounds":[{"type":"http","tag":"preserved","server":"127.0.0.1","server_port":8080,"username":"fixture","password":"credential-sentinel"}]}`)
	input.ConfigurationDigest, input.ConfigurationBytes, input.ConfigurationRevision = broker.Digest(raw), int64(len(raw)), updateDigest("complete-owner-snapshot")
	input.PreviousMaintenance = profile == "core"
	input.PreviousProvider = providerfixture.Identity(t, previousID).Source
	entry := manifest.Clients[0]
	info, err := os.Stat(oldPanel)
	if err != nil {
		t.Fatal(err)
	}
	peer := broker.PeerIdentity{PID: os.Getpid(), UID: 65534, GID: 65534, Executable: oldPanel, ExecutableDigest: entry.ExecutableDigest,
		Device: entry.Device, Inode: entry.Inode, ExecutableSize: info.Size()}
	input.PreviousExecutableDigest = peer.ExecutableDigest
	operationID := "update-operation:provider-maintenance"
	inbox, err := host.operationInbox(operationID, identity, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, identity.Artifacts[0].Role+"-"+identity.Artifacts[0].Name), archiveBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	prepare := PrepareRequestV1{Release: identity, ExpectedBrokerCapability: broker.CapabilityRevision, ExpectedManagementRevision: identity.DeploymentRevision, Maintenance: &input}
	envelope := func(payload any) broker.Request {
		return broker.Request{OperationID: operationID, Payload: mustJSON(payload), Expected: broker.Revisions{Configuration: identity.DeploymentRevision}}
	}
	value, err := host.prepare(t.Context(), envelope(prepare), peer)
	if err != nil {
		t.Fatal("actual source checkpoint failed", err)
	}
	checkpoint := value.(PrepareResultV1)
	state, err = host.loadState()
	if err != nil || checkpoint.ConfigurationVerified || !checkpoint.RollbackAvailable || state.ProviderMaintenance.Input != input || state.ProviderMaintenance.Phase != "CHECKPOINTED" {
		t.Fatal("hold was admitted without a durable complete checkpoint", err)
	}
	activate := ActivateRequestV1{Release: identity, PreparedRef: checkpoint.PreparedRef, RollbackRef: checkpoint.RollbackRef, ExpectedMode: "native",
		ConfigurationDigest: input.ConfigurationDigest, ConfigurationRevision: input.ConfigurationRevision, MaintenanceHeld: true}
	if _, err := host.activate(t.Context(), envelope(activate), peer); !brokerErrorCode(err, broker.CodeFence) {
		t.Fatal("unvalidated target was activated")
	}
	prepare.ValidateConfiguration, prepare.MaintenanceHeld = true, true
	if _, err := host.prepare(t.Context(), envelope(prepare), peer); err == nil {
		t.Fatal("missing configuration was validated")
	}
	for index, chunk := range [][]byte{raw[:17], raw[17:]} {
		offset := int64(0)
		if index == 1 {
			offset = 17
		}
		request := StageChunkRequestV1{Release: identity, Configuration: &input, Offset: offset, Chunk: chunk, Final: index == 1}
		value, err := host.stage(t.Context(), envelope(request), peer)
		if err != nil {
			t.Fatal("configuration stage failed", err)
		}
		result := value.(StageChunkResultV1)
		if result.AcceptedBytes != offset+int64(len(chunk)) || result.Complete != request.Final || result.ArtifactDigest != "" || request.Final && result.ConfigurationDigest != input.ConfigurationDigest {
			t.Fatal("configuration acknowledgment lost its exact content identity")
		}
	}
	value, err = host.prepare(t.Context(), envelope(prepare), peer)
	if err != nil || !value.(PrepareResultV1).ConfigurationVerified || value.(PrepareResultV1).ConfigurationRevision != input.ConfigurationRevision {
		t.Fatal("actual preserved configuration was not durably validated", err)
	}
	state, err = host.loadState()
	if err != nil || state.ProviderMaintenance.Phase != "VALIDATED" || !validDigest(state.ProviderMaintenance.ExecutableDigest) || bytes.Contains(mustJSON(state), []byte("credential-sentinel")) {
		t.Fatal("validation evidence is incomplete or contains raw configuration", err)
	}
	if _, err := os.Lstat(inbox); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("validated raw configuration was retained")
	}
	if _, err := host.prepare(t.Context(), envelope(prepare), peer); err != nil {
		t.Fatal("validation retry could not reopen durable evidence", err)
	}
	stale := activate
	stale.ConfigurationRevision = updateDigest("concurrent-configuration-change")
	if _, err := host.activate(t.Context(), envelope(stale), peer); !brokerErrorCode(err, broker.CodeFence) {
		t.Fatal("stale configuration activated a provider")
	}
	if _, err := host.activate(t.Context(), envelope(activate), peer); err != nil {
		t.Fatal("validated activation failed", err)
	}
	state, err = host.loadState()
	if err != nil || state.ProviderMaintenance.Phase != "ACTIVATED" || state.ActiveRelease != releaseDirectoryName(identity) || !state.hasBoundPreparedRollback() {
		t.Fatal("activation lost the prepared rollback authority", err)
	}
	verify := VerifyRequestV1{Release: identity, PreparedRef: checkpoint.PreparedRef, RollbackRef: checkpoint.RollbackRef}
	if _, err := host.verify(t.Context(), envelope(verify), peer); !brokerErrorCode(err, broker.CodeFence) {
		t.Fatal("a running process bypassed provider health proof")
	}
	rollback := RollbackRequestV1{Release: identity, RollbackRef: checkpoint.RollbackRef, ReasonCode: "qualification-after-activation"}
	if _, err := host.rollback(t.Context(), envelope(rollback), peer); err != nil {
		t.Fatal("provider rollback failed", err)
	}
	state, err = host.loadState()
	if err != nil || state.ActiveRelease != releaseDirectoryName(previous) || state.ProviderMaintenance != (providerMaintenanceState{}) || state.MaxActivatedSequence != identity.Sequence || state.MaxActivatedDigest != identity.ManifestDigest {
		t.Fatal("rollback lost the predecessor or reset global high-water authority", err)
	}
	t.Logf("previous_provider=%s target_provider=%s profile=%s target_sequence=%d target_manifest=%s prior_maintenance=%t configuration_sha256=%s", previousID, id, profile, identity.Sequence, identity.ManifestDigest, input.PreviousMaintenance, input.ConfigurationDigest)
	// These are handler/owner fixtures. Actual Panel+broker restart and runtime
	// health admission require the lifecycle/platform qualification, not a fake PID.
}
