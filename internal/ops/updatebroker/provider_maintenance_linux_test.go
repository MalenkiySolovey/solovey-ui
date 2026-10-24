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

	"github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilityview"
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
		PreparedRollbackOperationID: operationID, PreparedRollbackManifestDigest: identity.ManifestDigest, PreparedRollbackRef: SemanticReference("rollback", operationID, identity.ManifestDigest),
		ProviderMaintenance: providerMaintenanceState{OperationID: operationID, ManifestDigest: identity.ManifestDigest, ArtifactSetDigest: identity.ArtifactSetDigest,
			ReleaseDigest: broker.Digest(mustJSON(identity)), TargetRelease: releaseDirectoryName(identity), TargetSequence: identity.Sequence,
			TargetProvider: identity.CoreProvider, TargetPanelVersion: identity.Version, Input: input, Phase: "CHECKPOINTED"}}
}

func TestProviderMaintenanceEvidencePublicationReopensOnlyOldOrNewBoundState(t *testing.T) {
	requireRootUpdateBrokerTest(t)
	host := NewHost()
	host.StatePath = filepath.Join(t.TempDir(), "update-state.json")
	old := providerCheckpointState(t, providerReleaseFixture(t, "shtorm-extended"), providerMaintenanceFixture(t))
	next := old
	next.ProviderMaintenance.Phase = "VALIDATED"
	next.ProviderMaintenance.ExecutableDigest, next.ProviderMaintenance.ReportDigest = updateDigest("target-executable"), updateDigest("exact-check-report")
	capabilities := providerCapabilityFixture(t, "shtorm-extended", "full")
	next.ProviderMaintenance.TargetCapabilities, next.ProviderMaintenance.CapabilityRevision = string(mustJSON(capabilities)), capabilities.Revision()
	difference, err := capabilityview.Compare(providerCapabilityFixture(t, "sing-box", "full"), capabilities)
	if err != nil {
		t.Fatal(err)
	}
	next.ProviderMaintenance.PreviousReportDigest = updateDigest("previous-check-report")
	next.ProviderMaintenance.CapabilityDifference = string(mustJSON(difference))
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

func TestProviderMaintenanceActivationAndCommitRequireTheirRollbackBinding(t *testing.T) {
	identity := providerReleaseFixture(t, "shtorm-extended")
	state := providerCheckpointState(t, identity, providerMaintenanceFixture(t))
	proof := &state.ProviderMaintenance
	capabilities := providerCapabilityFixture(t, "shtorm-extended", "full")
	difference, err := capabilityview.Compare(providerCapabilityFixture(t, "sing-box", "full"), capabilities)
	if err != nil {
		t.Fatal(err)
	}
	proof.Phase, proof.ExecutableDigest, proof.ReportDigest = "ACTIVATED", updateDigest("executable"), updateDigest("report")
	proof.TargetCapabilities, proof.CapabilityRevision = string(mustJSON(capabilities)), capabilities.Revision()
	proof.PreviousReportDigest, proof.CapabilityDifference = updateDigest("previous-report"), string(mustJSON(difference))
	state.ActiveRelease, state.ActiveSequence, state.ActiveDigest = proof.TargetRelease, proof.TargetSequence, proof.ManifestDigest
	if err := validateProviderMaintenanceState(state); err != nil {
		t.Fatal("bound activation was rejected", err)
	}
	missingPrepared := state
	missingPrepared.clearPreparedRollback()
	if validateProviderMaintenanceState(missingPrepared) == nil {
		t.Fatal("activation without the prepared rollback authority was accepted")
	}
	state.ProviderMaintenance.Phase = "COMMITTED"
	if validateProviderMaintenanceState(state) == nil {
		t.Fatal("unpromoted, unverified activation was accepted as committed")
	}
	state.promotePreparedRollback()
	state.VerifiedSequence, state.VerifiedDigest = state.ActiveSequence, state.ActiveDigest
	if err := validateProviderMaintenanceState(state); err != nil {
		t.Fatal("committed runtime evidence with retained rollback was rejected", err)
	}
	for name, change := range map[string]func(*diskState){
		"rollback":        func(s *diskState) { s.clearRollback() },
		"runtime":         func(s *diskState) { s.VerifiedSequence, s.VerifiedDigest = 0, "" },
		"previous-report": func(s *diskState) { s.ProviderMaintenance.PreviousReportDigest = "" },
		"comparison":      func(s *diskState) { s.ProviderMaintenance.CapabilityDifference = "" },
		"comparison-source": func(s *diskState) {
			s.ProviderMaintenance.Input.PreviousCapabilityRevision = updateDigest("foreign-capabilities")
		},
	} {
		t.Run(name, func(t *testing.T) {
			invalid := state
			change(&invalid)
			if validateProviderMaintenanceState(invalid) == nil {
				t.Fatal("incomplete or mixed committed provider authority was accepted")
			}
		})
	}
}

func TestProviderMaintenanceRuntimeProofRequiresRestoredIntentAndCurrentHealth(t *testing.T) {
	proof := providerCheckpointState(t, providerReleaseFixture(t, "shtorm-extended"), providerMaintenanceFixture(t)).ProviderMaintenance
	proof.CapabilityRevision = providerCapabilityFixture(t, "shtorm-extended", "full").Revision()
	health := ProviderHealthV1{ConfigurationDigest: proof.Input.ConfigurationDigest, ConfigurationRevision: proof.Input.ConfigurationRevision,
		RuntimeState: "RUNNING", RuntimeGeneration: "generation-current", CapabilityRevision: proof.CapabilityRevision}
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
		func(h *ProviderHealthV1) { h.CapabilityRevision = updateDigest("other-binary-capabilities") },
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

func TestProviderRollbackRetainsPriorIntentUntilRuntimeVerification(t *testing.T) {
	state := providerCheckpointState(t, providerReleaseFixture(t, "shtorm-extended"), providerMaintenanceFixture(t))
	state.ProviderMaintenance.Phase = "ROLLED_BACK"
	if err := validateDiskState(state); err != nil {
		t.Fatal("pre-validation rollback lost its checkpoint", err)
	}
	observation := state.ProviderMaintenance.observation()
	if ValidateProviderMaintenanceObservation(*observation) != nil || observation.Input != state.ProviderMaintenance.Input ||
		observation.Input.PreviousPanelVersion == "" || observation.Input.PreviousCapabilityRevision == "" {
		t.Fatal("rollback restart cannot recover prior configuration and maintenance intent")
	}
	for _, mutate := range []func(*diskState){
		func(s *diskState) { s.clearPreparedRollback() },
		func(s *diskState) { s.ActiveDigest = updateDigest("other-active") },
		func(s *diskState) { s.ProviderMaintenance.Phase = "ROLLBACK_VERIFIED" },
		func(s *diskState) { s.ProviderMaintenance.ReportDigest = updateDigest("partial-target-evidence") },
		func(s *diskState) { s.ProviderMaintenance.Input.PreviousCapabilityRevision = "" },
	} {
		changed := state
		mutate(&changed)
		if validateDiskState(changed) == nil {
			t.Fatal("mixed rollback authority acquired restart verification")
		}
	}
	state.ProviderMaintenance.Phase = "ROLLBACK_VERIFIED"
	state.VerifiedSequence, state.VerifiedDigest = state.ActiveSequence, state.ActiveDigest
	if err := validateDiskState(state); err != nil || NewHost().legacyAutoVerificationAllowed(state) {
		t.Fatal("completed provider rollback was confused with legacy automatic verification", err)
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
	testProviderMaintenanceActualLifecycle(t, false)
}

func TestProviderMaintenanceActualInstallerCheckpointCheckActivateRollback(t *testing.T) {
	testProviderMaintenanceActualLifecycle(t, true)
}

func testProviderMaintenanceActualLifecycle(t *testing.T, installer bool) {
	t.Helper()
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
	previousName := releaseDirectoryName(previous)
	if installer {
		previousName = "installer-" + updateDigest("actual-installer-generation")[:16]
		if err := os.Rename(filepath.Join(host.ReleaseRoot, releaseDirectoryName(previous)), filepath.Join(host.ReleaseRoot, previousName)); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(host.ReleaseRoot, previousName, "release-identity.json")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(host.ReleaseRoot, previousName, "BUILD_INFO.txt"), []byte("profile="+profile+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldRoot := filepath.Join(host.ReleaseRoot, previousName)
	oldPanel := filepath.Join(oldRoot, "solovey-ui")
	manifest := publicationManifest(t, oldPanel)
	if err := os.WriteFile(host.Manifest, mustJSON(manifest), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := host.activateLink(previousName); err != nil {
		t.Fatal(err)
	}
	state := diskState{ActiveRelease: releaseDirectoryName(previous), ActiveSequence: previous.Sequence, ActiveDigest: previous.ManifestDigest,
		VerifiedSequence: previous.Sequence, VerifiedDigest: previous.ManifestDigest, MaxActivatedSequence: previous.Sequence, MaxActivatedDigest: previous.ManifestDigest}
	if installer {
		state = diskState{}
	} else if err := host.saveState(state); err != nil {
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
	object, err := openPreviousProviderPeer(peer, input, profile)
	if err != nil {
		t.Fatal(err)
	}
	previousCheck, checkErr := checkProviderObject(t.Context(), object, ReleaseIdentityV1{Version: input.PreviousPanelVersion,
		CoreProvider: input.PreviousProvider, BinaryProfile: profile}, raw, peer)
	closeErr := object.Close()
	if checkErr != nil || closeErr != nil {
		t.Fatal("previous actual executable observation failed", checkErr, closeErr)
	}
	input.PreviousCapabilityRevision = previousCheck.CapabilityRevision
	for _, mutate := range []func(*ProviderMaintenanceV1){
		func(v *ProviderMaintenanceV1) { v.PreviousCapabilityRevision = updateDigest("other-capabilities") },
		func(v *ProviderMaintenanceV1) { v.PreviousPanelVersion = "2026.4.999" },
	} {
		wrong := input
		mutate(&wrong)
		if check, err := checkPreviousProvider(t.Context(), wrong, profile, raw, peer); err == nil || check != (preparedProviderCheck{}) {
			t.Fatal("previous executable accepted a forged Panel version or capability revision")
		}
	}
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
	if installer && (state.ActiveRelease != previousName || state.ActiveSequence != 0 || state.ActiveDigest != "" ||
		state.PreparedRollbackSequence != 0 || state.PreparedRollbackDigest != "") {
		t.Fatal("installer checkpoint invented signed release provenance")
	}
	local := state.ActiveLocalCheckpoint
	if installer && (local == (LocalGenerationCheckpointV1{}) || state.PreparedRollbackLocalCheckpoint != local || !host.localCheckpointMatches(previousName, local)) {
		t.Fatal("installer predecessor has no durable content-bound checkpoint")
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
	if installer {
		metadata := filepath.Join(oldRoot, "BUILD_INFO.txt")
		original, err := os.ReadFile(metadata)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(metadata, append(append([]byte(nil), original...), []byte("changed=true\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := host.activate(t.Context(), envelope(activate), peer); !brokerErrorCode(err, broker.CodeRevision) {
			t.Fatal("changed installer contents retained rollback authority", err)
		}
		if err := os.WriteFile(metadata, original, 0o644); err != nil {
			t.Fatal(err)
		}
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
	if err != nil || state.ActiveRelease != previousName || state.ProviderMaintenance.Phase != "ROLLED_BACK" ||
		state.ProviderMaintenance.Input != input || !state.hasBoundPreparedRollback() || state.MaxActivatedSequence != identity.Sequence || state.MaxActivatedDigest != identity.ManifestDigest {
		t.Fatal("rollback lost the predecessor or reset global high-water authority", err)
	}
	if installer {
		if state.ActiveSequence != 0 || state.ActiveDigest != "" || state.ActiveLocalCheckpoint != local {
			t.Fatal("installer rollback invented signed release authority")
		}
		if _, err := os.Lstat(filepath.Join(oldRoot, "release-identity.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("installer rollback wrote synthetic release metadata", err)
		}
	}
	if _, err := os.Stat(filepath.Join(host.ReleaseRoot, releaseDirectoryName(identity), "release-identity.json")); err != nil {
		t.Fatal("rollback pruned the target needed to reconcile its checkpoint", err)
	}
	verify.Rollback = true
	verify.ProviderHealth = &ProviderHealthV1{ConfigurationDigest: input.ConfigurationDigest, ConfigurationRevision: input.ConfigurationRevision,
		Maintenance: input.PreviousMaintenance, RuntimeState: "RUNNING", RuntimeGeneration: "recovered-generation", CapabilityRevision: input.PreviousCapabilityRevision}
	if input.PreviousMaintenance {
		verify.ProviderHealth.RuntimeState, verify.ProviderHealth.RuntimeGeneration = "MAINTENANCE", ""
	}
	value, err = host.verify(t.Context(), envelope(verify), peer)
	if err != nil || value.(VerifyResultV1).Verified || value.(VerifyResultV1).HealthRevision != "restart-pending" {
		t.Fatal("a handler fixture was promoted to actual rollback runtime proof", err)
	}
	verify.ProviderHealth.CapabilityRevision = updateDigest("other-binary-capabilities")
	if _, err := host.verify(t.Context(), envelope(verify), peer); !brokerErrorCode(err, broker.CodeFence) {
		t.Fatal("unrelated capabilities were accepted as the predecessor's rollback health")
	}
	// An update within the same provider/profile can truthfully retain the
	// previous capability revision. Only a differing target is a negative case.
	if state.ProviderMaintenance.CapabilityRevision != input.PreviousCapabilityRevision {
		verify.ProviderHealth.CapabilityRevision = state.ProviderMaintenance.CapabilityRevision
		if _, err := host.verify(t.Context(), envelope(verify), peer); !brokerErrorCode(err, broker.CodeFence) {
			t.Fatal("different target capabilities were accepted as the predecessor's rollback health")
		}
	}
	t.Logf("previous_provider=%s target_provider=%s profile=%s target_sequence=%d target_manifest=%s prior_maintenance=%t configuration_sha256=%s", previousID, id, profile, identity.Sequence, identity.ManifestDigest, input.PreviousMaintenance, input.ConfigurationDigest)
	// These are handler/owner fixtures. Actual Panel+broker restart and runtime
	// health admission require the lifecycle/platform qualification, not a fake PID.
}
