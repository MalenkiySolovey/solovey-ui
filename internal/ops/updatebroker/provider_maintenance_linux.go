//go:build linux

package updatebroker

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/targetcheck"
)

// This is evidence on the existing root Update checkpoint, not another
// transaction or rollback owner. It contains no configuration bytes or paths.
type providerMaintenanceState struct {
	OperationID       string                     `json:"operationId"`
	ManifestDigest    string                     `json:"manifestDigest"`
	ArtifactSetDigest string                     `json:"artifactSetDigest"`
	ReleaseDigest     string                     `json:"releaseDigest"`
	TargetRelease     string                     `json:"targetRelease"`
	TargetSequence    uint64                     `json:"targetSequence"`
	TargetProvider    release.CoreProviderSource `json:"targetProvider"`
	Input             ProviderMaintenanceV1      `json:"input"`
	Phase             string                     `json:"phase"`
	ExecutableDigest  string                     `json:"executableDigest,omitempty"`
	ReportDigest      string                     `json:"reportDigest,omitempty"`
}

func (proof providerMaintenanceState) observation() *ProviderMaintenanceObservationV1 {
	if proof == (providerMaintenanceState{}) {
		return nil
	}
	value := ProviderMaintenanceObservationV1{OperationID: proof.OperationID, ManifestDigest: proof.ManifestDigest, ArtifactSetDigest: proof.ArtifactSetDigest,
		TargetSequence: proof.TargetSequence, TargetProvider: proof.TargetProvider, Input: proof.Input, Phase: proof.Phase,
		ExecutableDigest: proof.ExecutableDigest, ReportDigest: proof.ReportDigest}
	value.Revision = broker.Digest(mustJSON(value))
	return &value
}

func (proof providerMaintenanceState) matches(operationID string, identity ReleaseIdentityV1) bool {
	return proof.OperationID == operationID && proof.ManifestDigest == identity.ManifestDigest &&
		proof.ArtifactSetDigest == identity.ArtifactSetDigest && proof.TargetRelease == releaseDirectoryName(identity) &&
		proof.TargetSequence == identity.Sequence && proof.TargetProvider == identity.CoreProvider &&
		proof.ReleaseDigest == broker.Digest(mustJSON(identity))
}

func validateProviderMaintenanceState(state diskState) error {
	proof := state.ProviderMaintenance
	if proof == (providerMaintenanceState{}) {
		return nil
	}
	failed := errors.New("provider maintenance checkpoint state is invalid")
	if !safeOperationID(proof.OperationID) || !validDigest(proof.ManifestDigest) || !validDigest(proof.ArtifactSetDigest) ||
		!validDigest(proof.ReleaseDigest) || !releaseDirectoryPattern.MatchString(proof.TargetRelease) || proof.TargetSequence == 0 ||
		release.ValidateCoreProviderSource(proof.TargetProvider) != nil || ValidateProviderMaintenance(proof.Input) != nil ||
		proof.TargetRelease != providerReleaseDirectoryName(ReleaseIdentityV1{Sequence: proof.TargetSequence, ManifestDigest: proof.ManifestDigest, ArtifactSetDigest: proof.ArtifactSetDigest}) {
		return failed
	}
	rollbackRef := semanticRef("rollback", proof.OperationID, proof.ManifestDigest)
	if proof.Phase != "CHECKPOINTED" && (!validDigest(proof.ExecutableDigest) || !validDigest(proof.ReportDigest)) {
		return failed
	}
	switch proof.Phase {
	case "CHECKPOINTED", "VALIDATED":
		if !state.hasBoundPreparedRollback() || !state.preparedRollbackMatches(proof.OperationID, proof.ManifestDigest, rollbackRef) ||
			proof.Phase == "CHECKPOINTED" && (proof.ExecutableDigest != "" || proof.ReportDigest != "") {
			return failed
		}
	case "ACTIVATED", "COMMITTED":
		if state.ActiveRelease != proof.TargetRelease || state.ActiveSequence != proof.TargetSequence || state.ActiveDigest != proof.ManifestDigest {
			return failed
		}
		if proof.Phase == "ACTIVATED" && !state.preparedRollbackMatches(proof.OperationID, proof.ManifestDigest, rollbackRef) ||
			proof.Phase == "COMMITTED" && (!state.rollbackMatches(proof.OperationID, proof.ManifestDigest, rollbackRef) ||
				state.VerifiedSequence != proof.TargetSequence || state.VerifiedDigest != proof.ManifestDigest) {
			return failed
		}
	default:
		return failed
	}
	return nil
}

func (h *Host) legacyAutoVerificationAllowed(state diskState) bool {
	if state.ProviderMaintenance != (providerMaintenanceState{}) {
		return false
	}
	// Qualified names remain fail-closed even if their identity file is lost.
	if len(state.ActiveRelease) > 38 && releaseDirectoryPattern.MatchString(state.ActiveRelease) {
		return false
	}
	root := filepath.Join(h.ReleaseRoot, state.ActiveRelease)
	if _, err := os.Lstat(filepath.Join(root, "release-identity.json")); errors.Is(err, os.ErrNotExist) {
		return true // Released v1/installer generations can lack this metadata.
	}
	identity, err := loadReleaseIdentity(root)
	return err == nil && identity.CoreProvider == (release.CoreProviderSource{})
}

func verifyPreviousProviderPeer(peer broker.PeerIdentity, input ProviderMaintenanceV1, profile string) error {
	failed := errors.New("provider maintenance source observation changed")
	if peer.PID <= 0 || peer.ExecutableDigest != input.PreviousExecutableDigest || ValidateProviderMaintenance(input) != nil {
		return failed
	}
	object, err := executableobject.Open(peer.Executable, executableobject.Policy{
		MaxBytes: release.MaxPanelMemberBytes, RequireRegular: true, RequireExecutable: true,
		RequireRootOwner: true, RequiredMode: 0o755, ForbiddenMode: 0o022,
		RequireTrustedAncestry: true, AncestryOwner: 0, AncestryForbiddenMode: 0o022,
		AllowRootOwnedStickyAncestry: true, RequireStablePath: true,
	})
	if err != nil {
		return failed
	}
	defer func() { _ = object.Close() }()
	identity := object.Identity()
	observed, err := release.InspectPanelBuild(object.File(), input.PreviousProvider)
	if err != nil || identity.Digest != peer.ExecutableDigest || identity.Device != peer.Device || identity.Inode != peer.Inode ||
		identity.Size != peer.ExecutableSize || observed.CoreProvider != input.PreviousProvider || observed.BinaryProfile != profile || object.Revalidate() != nil {
		return failed
	}
	return nil
}

func (h *Host) stageProviderConfiguration(envelope broker.Request, request StageChunkRequestV1, peer broker.PeerIdentity) (any, error) {
	if request.Artifact != (ArtifactIdentityV1{}) || request.ExpectedPrefix != "" || request.Configuration == nil ||
		ValidateProviderMaintenance(*request.Configuration) != nil || request.Release.CoreProvider == (release.CoreProviderSource{}) || len(request.Release.Artifacts) != 1 {
		return nil, broker.Failure(broker.CodeInvalidRequest, "provider configuration chunk is invalid")
	}
	state, err := h.loadState()
	proof := state.ProviderMaintenance
	if err != nil || proof.Phase != "CHECKPOINTED" || !proof.matches(envelope.OperationID, request.Release) ||
		proof.Input != *request.Configuration || peer.PID <= 0 || peer.ExecutableDigest != proof.Input.PreviousExecutableDigest ||
		!state.preparedRollbackMatches(envelope.OperationID, request.Release.ManifestDigest, semanticRef("rollback", envelope.OperationID, request.Release.ManifestDigest)) {
		return nil, broker.Failure(broker.CodeFence, "provider configuration checkpoint changed")
	}
	accepted, err := validateChunkAdmission(request.Offset, len(request.Chunk), proof.Input.ConfigurationBytes, request.Final)
	if err != nil {
		return nil, broker.Failure(broker.CodeInvalidRequest, "provider configuration chunk exceeds its declared size")
	}
	if err := h.reconcileStagingState(envelope.OperationID); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update staging state could not be reconciled")
	}
	root, err := h.operationInbox(envelope.OperationID, request.Release, false)
	if err != nil {
		return nil, broker.Failure(broker.CodeExecution, "provider configuration inbox is unavailable")
	}
	result, err := h.stageFixedContent(root, filepath.Join(root, ".provider-configuration.json"), request.Offset, request.Chunk,
		request.Final, accepted, proof.Input.ConfigurationDigest)
	if err != nil {
		return nil, err
	}
	result.ConfigurationDigest, result.ArtifactDigest = result.ArtifactDigest, ""
	return result, nil
}

// The second Prepare phase consumes only the configuration already bound by
// the first phase's durable root checkpoint. It cannot rewrite that checkpoint.
func (h *Host) prepareProviderConfiguration(ctx context.Context, envelope broker.Request, request PrepareRequestV1, peer broker.PeerIdentity) (any, error) {
	if request.Maintenance == nil || !request.MaintenanceHeld || ValidateProviderMaintenance(*request.Maintenance) != nil {
		return nil, broker.Failure(broker.CodeInvalidRequest, "provider configuration preflight lacks a maintenance hold")
	}
	state, err := h.loadState()
	proof := state.ProviderMaintenance
	if err != nil || !proof.matches(envelope.OperationID, request.Release) || proof.Input != *request.Maintenance ||
		(proof.Phase != "CHECKPOINTED" && proof.Phase != "VALIDATED") ||
		!state.preparedRollbackMatches(envelope.OperationID, request.Release.ManifestDigest, semanticRef("rollback", envelope.OperationID, request.Release.ManifestDigest)) ||
		verifyPreviousProviderPeer(peer, proof.Input, request.Release.BinaryProfile) != nil {
		return nil, broker.Failure(broker.CodeFence, "provider configuration checkpoint changed")
	}
	name, stored, err := h.resolveReleaseGeneration(request.Release)
	if err != nil || !sameReleaseIdentity(stored, request.Release, true) {
		return nil, broker.Failure(broker.CodeRevision, "provider configuration target changed")
	}
	root := filepath.Join(h.ReleaseRoot, name)
	if proof.Phase == "CHECKPOINTED" {
		inbox, err := h.operationInbox(envelope.OperationID, request.Release, false)
		if err != nil {
			return nil, broker.Failure(broker.CodeExecution, "provider configuration inbox is unavailable")
		}
		raw, err := readProviderConfiguration(filepath.Join(inbox, ".provider-configuration.json"), proof.Input)
		if err != nil {
			return nil, broker.Failure(broker.CodeRevision, "provider configuration input changed")
		}
		check, err := checkPreparedProvider(ctx, root, stored, raw, peer)
		clear(raw)
		if err != nil {
			return nil, broker.Failure(broker.CodeExecution, "provider target rejected the preserved configuration")
		}
		proof.Phase, proof.ExecutableDigest, proof.ReportDigest = "VALIDATED", check.ExecutableDigest, check.ReportDigest
		state.ProviderMaintenance = proof
		if err := h.saveState(state); err != nil {
			return nil, broker.Failure(broker.CodeExecution, "provider configuration evidence could not be committed")
		}
	}
	object, err := openPreparedProvider(root, stored)
	if err != nil {
		return nil, broker.Failure(broker.CodeExecution, "validated provider executable is unavailable")
	}
	digest := object.Identity().Digest
	closeErr := object.Close()
	if closeErr != nil || digest != proof.ExecutableDigest {
		return nil, broker.Failure(broker.CodeRevision, "validated provider executable changed")
	}
	// Raw credentials are no longer needed once descriptor-bound evidence is
	// durable. A retry can reopen that evidence after successful inbox removal.
	inbox := filepath.Join(h.InboxRoot, envelope.OperationID)
	if _, err := h.inboxOps.lstat(inbox); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return nil, broker.Failure(broker.CodeExecution, "provider configuration inbox is unsafe")
		}
		if _, err := h.operationInbox(envelope.OperationID, stored, false); err != nil || h.removeOwnedInbox(inbox) != nil {
			return nil, broker.Failure(broker.CodeExecution, "provider configuration inbox could not be released")
		}
	}
	result := providerPrepareResult(envelope.OperationID, stored, true)
	result.ConfigurationRevision = proof.Input.ConfigurationRevision
	return result, nil
}

func providerPrepareResult(operationID string, identity ReleaseIdentityV1, validated bool) PrepareResultV1 {
	result := PrepareResultV1{ProviderRevision: ProviderRevision, PreparedRef: semanticRef("prepared", operationID, identity.ManifestDigest),
		RollbackRef: semanticRef("rollback", operationID, identity.ManifestDigest), RollbackAvailable: true, ManagementReady: true,
		PreflightRevision: broker.Digest([]byte(releaseDirectoryName(identity) + ":" + identity.DeploymentRevision)), ConfigurationVerified: validated}
	return result
}

func readProviderConfiguration(path string, input ProviderMaintenanceV1) ([]byte, error) {
	failed := errors.New("provider configuration input is unsafe")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || info.Size() != input.ConfigurationBytes {
		return nil, failed
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return nil, failed
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, failed
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, failed
	}
	raw, err := io.ReadAll(io.LimitReader(file, targetcheck.MaxConfigBytes+1))
	after, statErr := file.Stat()
	finalPath, pathErr := os.Lstat(path)
	if err != nil || statErr != nil || !os.SameFile(info, after) || opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) ||
		pathErr != nil || !os.SameFile(info, finalPath) || finalPath.Mode() != info.Mode() ||
		int64(len(raw)) != input.ConfigurationBytes || broker.Digest(raw) != input.ConfigurationDigest {
		clear(raw)
		return nil, failed
	}
	return raw, nil
}

func (h *Host) admitProviderActivation(envelope broker.Request, request ActivateRequestV1, stored ReleaseIdentityV1, state diskState, peer broker.PeerIdentity) error {
	if stored.CoreProvider == (release.CoreProviderSource{}) {
		if state.ProviderMaintenance != (providerMaintenanceState{}) {
			return errors.New("provider maintenance admission is required")
		}
		return nil
	}
	proof := state.ProviderMaintenance
	if !proof.matches(envelope.OperationID, stored) || proof.Phase != "VALIDATED" || !request.MaintenanceHeld ||
		request.ConfigurationDigest != proof.Input.ConfigurationDigest || request.ConfigurationRevision != proof.Input.ConfigurationRevision ||
		verifyPreviousProviderPeer(peer, proof.Input, stored.BinaryProfile) != nil {
		return errors.New("provider activation maintenance proof changed")
	}
	object, err := openPreparedProvider(filepath.Join(h.ReleaseRoot, proof.TargetRelease), stored)
	if err != nil {
		return err
	}
	digest := object.Identity().Digest
	closeErr := object.Close()
	if closeErr != nil || digest != proof.ExecutableDigest {
		return errors.New("provider activation executable changed")
	}
	return nil
}

func providerHealthMatches(proof providerMaintenanceState, health *ProviderHealthV1) bool {
	if health == nil || health.ConfigurationDigest != proof.Input.ConfigurationDigest || health.ConfigurationRevision != proof.Input.ConfigurationRevision ||
		health.Maintenance != proof.Input.PreviousMaintenance || !validDigest(health.CapabilityRevision) || !validRuntimeGeneration(health.RuntimeGeneration) {
		return false
	}
	return health.Maintenance && health.RuntimeState == "MAINTENANCE" ||
		!health.Maintenance && health.RuntimeState == "RUNNING" && health.RuntimeGeneration != ""
}
