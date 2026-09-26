//go:build linux

package updatebroker

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

var (
	releaseDirectoryPattern = regexp.MustCompile(`^(?:[0-9]{20}|installer)-[a-f0-9]{16}$`)
	stagingDirectoryPattern = regexp.MustCompile(`^(?:[0-9]{20}|installer)-[a-f0-9]{16}\.staging$`)
)

const (
	defaultInboxRoot       = "/var/lib/solovey-ui-broker/update-inbox"
	defaultReleaseRoot     = "/usr/local/solovey-ui/releases"
	defaultStatePath       = "/var/lib/solovey-ui-broker/update-state.json"
	defaultManifest        = "/etc/solovey-ui/broker-clients.json"
	maxResumableInboxCount = 1
	maxResumableInboxBytes = int64(1<<30 + 64<<10)
	maxResumableInboxAge   = 24 * time.Hour
)

type diskState struct {
	ActiveSequence                 uint64 `json:"activeSequence"`
	ActiveDigest                   string `json:"activeDigest"`
	VerifiedSequence               uint64 `json:"verifiedSequence"`
	VerifiedDigest                 string `json:"verifiedDigest"`
	MaxActivatedSequence           uint64 `json:"maxActivatedSequence,omitempty"`
	MaxActivatedDigest             string `json:"maxActivatedDigest,omitempty"`
	ActiveRelease                  string `json:"activeRelease"`
	RollbackRelease                string `json:"rollbackRelease,omitempty"`
	RollbackSequence               uint64 `json:"rollbackSequence,omitempty"`
	RollbackDigest                 string `json:"rollbackDigest,omitempty"`
	RollbackOperationID            string `json:"rollbackOperationId,omitempty"`
	RollbackManifestDigest         string `json:"rollbackManifestDigest,omitempty"`
	RollbackRef                    string `json:"rollbackRef,omitempty"`
	PreparedRollbackRelease        string `json:"preparedRollbackRelease,omitempty"`
	PreparedRollbackSequence       uint64 `json:"preparedRollbackSequence,omitempty"`
	PreparedRollbackDigest         string `json:"preparedRollbackDigest,omitempty"`
	PreparedRollbackOperationID    string `json:"preparedRollbackOperationId,omitempty"`
	PreparedRollbackManifestDigest string `json:"preparedRollbackManifestDigest,omitempty"`
	PreparedRollbackRef            string `json:"preparedRollbackRef,omitempty"`
	UpdatedAt                      int64  `json:"updatedAt"`
}

type Host struct {
	InboxRoot     string
	ReleaseRoot   string
	StatePath     string
	Manifest      string
	Now           func() time.Time
	Restart       func(panelPID int)
	OwnerManifest func(context.Context, string, string) error
	saveStateFn   func(diskState) error
	atomicOps     updateAtomicOps
	generationOps updateGenerationOps
	inboxOps      updateInboxOps
}

func NewHost() *Host {
	return &Host{InboxRoot: defaultInboxRoot, ReleaseRoot: defaultReleaseRoot, StatePath: defaultStatePath,
		Manifest: defaultManifest, Now: time.Now, Restart: scheduleReleaseRestart, OwnerManifest: writeOwnerManifest,
		atomicOps: productionUpdateAtomicOps, generationOps: productionUpdateGenerationOps, inboxOps: productionUpdateInboxOps}
}

func registerNativeHandlers(registry *broker.Registry) error {
	host := NewHost()
	definitions := []struct {
		verb     broker.Verb
		mutation bool
		handler  broker.Handler
	}{
		{broker.VerbUpdateObserve, false, host.observe},
		{broker.VerbUpdateStage, true, host.stage},
		{broker.VerbUpdateRelease, true, host.releaseStaging},
		{broker.VerbUpdatePrepare, true, host.prepare},
		{broker.VerbUpdateActivate, true, host.activate},
		{broker.VerbUpdateVerify, true, host.verify},
		{broker.VerbUpdateRollback, true, host.rollback},
	}
	for _, definition := range definitions {
		if err := registry.Register(definition.verb, broker.Definition{Role: broker.RolePanel, Mutation: definition.mutation, Handler: definition.handler}); err != nil {
			return broker.StartupFailure("update", "update.lifecycle", "native-self-managed", "handler_registration_failed", err)
		}
	}
	return nil
}

func (h *Host) observe(_ context.Context, _ broker.Request, peer broker.PeerIdentity) (any, error) {
	if err := h.reconcileStagingState(""); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update staging state could not be reconciled")
	}
	state, err := h.loadState()
	if err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update state is unavailable")
	}
	managementReady := peer.PID > 0
	verified := h.runningRelease(state.ActiveRelease, peer)
	if verified {
		if state.preparedRollbackMatches(state.PreparedRollbackOperationID, state.ActiveDigest, state.PreparedRollbackRef) {
			state.promotePreparedRollback()
		}
		state.VerifiedSequence, state.VerifiedDigest = state.ActiveSequence, state.ActiveDigest
		state.raiseHighWater(state.ActiveSequence, state.ActiveDigest)
		if err := h.persistState(state); err != nil {
			return nil, broker.Failure(broker.CodeExecution, "verified update state could not be persisted")
		}
	}
	result := ObservationV1{ProviderRevision: ProviderRevision, InstalledSequence: state.ActiveSequence,
		ActiveSequence: state.ActiveSequence, ActiveDigest: state.ActiveDigest, InstalledDigest: state.ActiveDigest,
		VerifiedSequence: state.VerifiedSequence, VerifiedDigest: state.VerifiedDigest,
		MaxActivatedSequence: state.MaxActivatedSequence, MaxActivatedDigest: state.MaxActivatedDigest,
		RollbackAvailable: state.hasBoundRollback(), RollbackOperationID: state.RollbackOperationID,
		RollbackManifestDigest: state.RollbackManifestDigest, RollbackRef: state.RollbackRef,
		RollbackTargetSequence: state.RollbackSequence, RollbackTargetDigest: state.RollbackDigest,
		PreparedRollbackAvailable: state.hasBoundPreparedRollback(), PreparedRollbackOperationID: state.PreparedRollbackOperationID,
		PreparedRollbackManifestDigest: state.PreparedRollbackManifestDigest, PreparedRollbackRef: state.PreparedRollbackRef,
		PreparedRollbackTargetSequence: state.PreparedRollbackSequence, PreparedRollbackTargetDigest: state.PreparedRollbackDigest,
		ManagementReady: managementReady, ObservedAt: h.Now().Unix()}
	result.Revision = broker.Digest(mustJSON(result))
	return result, nil
}

func (h *Host) stage(_ context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request StageChunkRequestV1
	if err := broker.DecodeRawPayload(envelope.Payload, &request); err != nil || !safeOperationID(envelope.OperationID) ||
		ValidateRelease(request.Release) != nil || ValidateArtifact(request.Artifact) != nil || len(request.Chunk) == 0 || len(request.Chunk) > MaxChunkBytes || request.Offset < 0 {
		return nil, broker.Failure(broker.CodeInvalidRequest, "update artifact chunk is invalid")
	}
	if request.Release.DeploymentRevision != envelope.Expected.Configuration && envelope.Expected.Configuration != "" {
		return nil, broker.Failure(broker.CodeRevision, "update release revision changed")
	}
	declared := false
	for _, artifact := range request.Release.Artifacts {
		if artifact == request.Artifact {
			declared = true
			break
		}
	}
	if !declared {
		return nil, broker.Failure(broker.CodeInvalidRequest, "update artifact is outside the declared release set")
	}
	accepted, err := validateChunkAdmission(request.Offset, len(request.Chunk), request.Artifact.Size, request.Final)
	if err != nil {
		return nil, broker.Failure(broker.CodeInvalidRequest, "update artifact chunk exceeds its declared size")
	}
	if err := h.reconcileStagingState(envelope.OperationID); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update staging state could not be reconciled")
	}
	root, err := h.operationInbox(envelope.OperationID, request.Release, true)
	if err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update inbox is unavailable")
	}
	if err := h.reconcileStagingState(envelope.OperationID); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update inbox bounds could not be reconciled")
	}
	path := filepath.Join(root, request.Artifact.Role+"-"+request.Artifact.Name)
	if err := ensureRegularOrAbsent(path); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update artifact target is unsafe")
	}
	file, err := h.inboxOps.openFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update artifact could not be staged")
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() != request.Offset && info.Size() != accepted {
		_ = file.Close()
		return nil, broker.Failure(broker.CodeFence, "update artifact offset changed")
	}
	if info.Size() == accepted {
		replayed := make([]byte, len(request.Chunk))
		count, readErr := file.ReadAt(replayed, request.Offset)
		if readErr != nil || count != len(replayed) || !bytes.Equal(replayed, request.Chunk) {
			_ = file.Close()
			return nil, broker.Failure(broker.CodeFence, "update artifact replay changed")
		}
	} else if _, err = file.WriteAt(request.Chunk, request.Offset); err != nil {
		_ = file.Close()
		return nil, broker.Failure(broker.CodeExecution, "update artifact chunk was not committed")
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return nil, broker.Failure(broker.CodeExecution, "update artifact chunk was not committed")
	}
	if err := h.inboxOps.syncDirectory(root); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update artifact name was not committed")
	}
	result := StageChunkResultV1{ProviderRevision: ProviderRevision, AcceptedBytes: accepted}
	if request.Final {
		if accepted != request.Artifact.Size {
			return nil, broker.Failure(broker.CodeInvalidRequest, "update artifact final size is invalid")
		}
		digest, err := fileDigest(path)
		if err != nil || digest != request.Artifact.SHA256 {
			return nil, broker.Failure(broker.CodeRevision, "update artifact digest is invalid")
		}
		result.Complete, result.ArtifactDigest = true, digest
	}
	return result, nil
}

func (h *Host) prepare(_ context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request PrepareRequestV1
	if err := broker.DecodeRawPayload(envelope.Payload, &request); err != nil || !safeOperationID(envelope.OperationID) ||
		ValidateRelease(request.Release) != nil || request.ExpectedBrokerCapability != broker.CapabilityRevision ||
		!validDigest(request.ExpectedManagementRevision) || request.ExpectedManagementRevision != request.Release.DeploymentRevision {
		return nil, broker.Failure(broker.CodeInvalidRequest, "update preflight request is invalid")
	}
	if err := h.reconcileStagingState(envelope.OperationID); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update staging state could not be reconciled")
	}
	inbox, err := h.operationInbox(envelope.OperationID, request.Release, false)
	if err != nil || len(request.Release.Artifacts) == 0 {
		return nil, broker.Failure(broker.CodeExecution, "update inbox is unavailable")
	}
	var panelArtifact string
	for _, artifact := range request.Release.Artifacts {
		path := filepath.Join(inbox, artifact.Role+"-"+artifact.Name)
		actual, digestErr := fileDigest(path)
		if digestErr != nil || actual != artifact.SHA256 {
			return nil, broker.Failure(broker.CodeRevision, "prepared release artifact is incomplete")
		}
		if artifact.Role == "panel-"+request.Release.BinaryProfile {
			panelArtifact = path
		}
	}
	if panelArtifact == "" {
		return nil, broker.Failure(broker.CodeInvalidRequest, "prepared release lacks panel payload")
	}
	releaseName := releaseDirectoryName(request.Release)
	finalRoot := filepath.Join(h.ReleaseRoot, releaseName)
	state, stateErr := h.loadState()
	if stateErr != nil {
		return nil, broker.Failure(broker.CodeExecution, "update state is unavailable")
	}
	if state.ActiveRelease == "" {
		if current, linkErr := os.Readlink(filepath.Join(h.ReleaseRoot, "current")); linkErr == nil {
			candidate := filepath.Base(current)
			identity, identityErr := loadReleaseIdentity(filepath.Join(h.ReleaseRoot, candidate))
			if candidate == current && identityErr == nil && h.verifyReleaseExecutables(filepath.Join(h.ReleaseRoot, candidate)) == nil {
				state.ActiveRelease, state.ActiveSequence, state.ActiveDigest = candidate, identity.Sequence, identity.ManifestDigest
			}
		}
	}
	maxSequence, maxDigest := state.activationHighWater()
	rollbackRef := semanticRef("rollback", envelope.OperationID, request.Release.ManifestDigest)
	preparedReplay := state.preparedRollbackMatches(envelope.OperationID, request.Release.ManifestDigest, rollbackRef)
	activatedReplay := preparedReplay && state.ActiveSequence == request.Release.Sequence && state.ActiveDigest == request.Release.ManifestDigest
	if request.Release.Sequence < maxSequence || request.Release.Sequence == maxSequence && !activatedReplay {
		return nil, broker.Failure(broker.CodeRevision, "prepared release sequence is not monotonic")
	}
	if state.hasBoundPreparedRollback() && !preparedReplay {
		return nil, broker.Failure(broker.CodeFence, "another update rollback checkpoint is still prepared")
	}
	if info, err := os.Lstat(finalRoot); errors.Is(err, os.ErrNotExist) {
		if err := h.publishPreparedGeneration(panelArtifact, finalRoot, request.Release); err != nil {
			return nil, broker.Failure(broker.CodeExecution, "release generation could not be committed")
		}
	} else if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, broker.Failure(broker.CodeExecution, "prepared release target is unsafe")
	} else if stored, err := loadReleaseIdentity(finalRoot); err != nil || !sameReleaseIdentity(stored, request.Release, true) ||
		h.verifyPreparedReleaseExecutables(finalRoot, request.Release.BinaryProfile) != nil || verifyReleaseTree(finalRoot) != nil {
		return nil, broker.Failure(broker.CodeRevision, "prepared release identity changed")
	}
	if !preparedReplay {
		state.PreparedRollbackRelease, state.PreparedRollbackSequence, state.PreparedRollbackDigest = state.ActiveRelease, state.ActiveSequence, state.ActiveDigest
		state.PreparedRollbackOperationID, state.PreparedRollbackManifestDigest, state.PreparedRollbackRef = envelope.OperationID, request.Release.ManifestDigest, rollbackRef
	}
	if state.MaxActivatedSequence < maxSequence {
		state.MaxActivatedSequence, state.MaxActivatedDigest = maxSequence, maxDigest
	}
	state.UpdatedAt = h.Now().Unix()
	if err := h.saveState(state); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "release rollback checkpoint could not be committed")
	}
	if err := h.removeOwnedInbox(inbox); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "prepared operation inbox could not be released")
	}
	return PrepareResultV1{ProviderRevision: ProviderRevision, PreparedRef: semanticRef("prepared", envelope.OperationID, request.Release.ManifestDigest),
		RollbackRef: rollbackRef, RollbackAvailable: state.hasBoundPreparedRollback(), ManagementReady: true,
		PreflightRevision: broker.Digest([]byte(releaseName + ":" + request.ExpectedManagementRevision))}, nil
}

func (h *Host) activate(ctx context.Context, envelope broker.Request, peer broker.PeerIdentity) (any, error) {
	var request ActivateRequestV1
	if err := broker.DecodeRawPayload(envelope.Payload, &request); err != nil || !safeOperationID(envelope.OperationID) || ValidateRelease(request.Release) != nil || request.ExpectedMode != "native" ||
		request.PreparedRef != semanticRef("prepared", envelope.OperationID, request.Release.ManifestDigest) ||
		request.RollbackRef != semanticRef("rollback", envelope.OperationID, request.Release.ManifestDigest) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "update activation request is invalid")
	}
	releaseName := releaseDirectoryName(request.Release)
	releaseRoot := filepath.Join(h.ReleaseRoot, releaseName)
	stored, identityErr := loadReleaseIdentity(releaseRoot)
	if err := h.verifyPreparedReleaseExecutables(releaseRoot, request.Release.BinaryProfile); err != nil || identityErr != nil || !sameReleaseIdentity(stored, request.Release, false) {
		return nil, broker.Failure(broker.CodeExecution, "prepared release is unavailable")
	}
	state, err := h.loadState()
	if err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update state is unavailable")
	}
	if !state.preparedRollbackMatches(envelope.OperationID, request.Release.ManifestDigest, request.RollbackRef) {
		return nil, broker.Failure(broker.CodeFence, "prepared rollback authority changed")
	}
	maxSequence, _ := state.activationHighWater()
	if request.Release.Sequence < maxSequence || request.Release.Sequence == maxSequence &&
		(state.ActiveSequence != request.Release.Sequence || state.ActiveDigest != request.Release.ManifestDigest) {
		return nil, broker.Failure(broker.CodeRevision, "activated release sequence is not monotonic")
	}
	previousRelease := state.ActiveRelease
	if err := h.activateLink(releaseName); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "release activation could not be committed")
	}
	if err := h.rewriteClientManifest(releaseRoot); err != nil {
		h.restoreActivation(ctx, previousRelease)
		return nil, broker.Failure(broker.CodeExecution, "broker client manifest could not be rotated")
	}
	if h.OwnerManifest == nil || h.OwnerManifest(ctx, releaseRoot, request.Release.BinaryProfile) != nil {
		h.restoreActivation(ctx, previousRelease)
		return nil, broker.Failure(broker.CodeExecution, "application owner manifest could not be rotated")
	}
	state.ActiveRelease, state.ActiveSequence, state.ActiveDigest = releaseName, request.Release.Sequence, request.Release.ManifestDigest
	state.VerifiedSequence, state.VerifiedDigest, state.UpdatedAt = 0, "", h.Now().Unix()
	state.raiseHighWater(request.Release.Sequence, request.Release.ManifestDigest)
	if err := h.saveState(state); err != nil {
		// A rename-visible state publication already names the new authority.
		// Restoring only the other projections would manufacture a mixed
		// generation, so leave the complete new projection in place for exact
		// restart reconciliation at this explicitly uncertain boundary.
		if !updateAuthorityVisible(err) {
			h.restoreActivation(ctx, previousRelease)
		}
		return nil, broker.Failure(broker.CodeExecution, "active release state could not be committed")
	}
	if h.Restart != nil {
		h.Restart(peer.PID)
	}
	return ActivateResultV1{ProviderRevision: ProviderRevision, ActiveSequence: request.Release.Sequence,
		ActiveDigest: request.Release.ManifestDigest, RestartRequired: true}, nil
}

func (h *Host) verify(_ context.Context, envelope broker.Request, peer broker.PeerIdentity) (any, error) {
	var request VerifyRequestV1
	if err := broker.DecodeRawPayload(envelope.Payload, &request); err != nil || !safeOperationID(envelope.OperationID) || ValidateRelease(request.Release) != nil ||
		request.PreparedRef != semanticRef("prepared", envelope.OperationID, request.Release.ManifestDigest) ||
		request.RollbackRef != semanticRef("rollback", envelope.OperationID, request.Release.ManifestDigest) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "active release verification request is invalid")
	}
	state, err := h.loadState()
	if err != nil || state.ActiveSequence != request.Release.Sequence || state.ActiveDigest != request.Release.ManifestDigest {
		return nil, broker.Failure(broker.CodeRevision, "active release identity changed")
	}
	preparedAuthority := state.preparedRollbackMatches(envelope.OperationID, request.Release.ManifestDigest, request.RollbackRef)
	if !preparedAuthority && !state.rollbackMatches(envelope.OperationID, request.Release.ManifestDigest, request.RollbackRef) {
		return nil, broker.Failure(broker.CodeFence, "active rollback authority changed")
	}
	stored, identityErr := loadReleaseIdentity(filepath.Join(h.ReleaseRoot, state.ActiveRelease))
	if identityErr != nil || !sameReleaseIdentity(stored, request.Release, false) {
		return nil, broker.Failure(broker.CodeRevision, "active release metadata changed")
	}
	verified := h.runningRelease(state.ActiveRelease, peer)
	healthRevision := "restart-pending"
	if verified {
		if preparedAuthority {
			state.promotePreparedRollback()
		}
		state.VerifiedSequence, state.VerifiedDigest, state.UpdatedAt = state.ActiveSequence, state.ActiveDigest, h.Now().Unix()
		state.raiseHighWater(state.ActiveSequence, state.ActiveDigest)
		if err := h.saveState(state); err != nil {
			return nil, broker.Failure(broker.CodeExecution, "verified release state could not be committed")
		}
		healthRevision = broker.Digest([]byte(state.ActiveRelease + ":management-ready"))
		h.pruneReleases(state.ActiveRelease, state.RollbackRelease)
	}
	return VerifyResultV1{ProviderRevision: ProviderRevision, Verified: verified, VerifiedSequence: state.VerifiedSequence,
		VerifiedDigest: state.VerifiedDigest, ManagementReady: peer.PID > 0, HealthRevision: healthRevision}, nil
}

func (h *Host) rollback(ctx context.Context, envelope broker.Request, peer broker.PeerIdentity) (any, error) {
	var request RollbackRequestV1
	if err := broker.DecodeRawPayload(envelope.Payload, &request); err != nil || !safeOperationID(envelope.OperationID) || ValidateRelease(request.Release) != nil ||
		request.RollbackRef != semanticRef("rollback", envelope.OperationID, request.Release.ManifestDigest) || len(request.ReasonCode) > 96 {
		return nil, broker.Failure(broker.CodeInvalidRequest, "update rollback request is invalid")
	}
	state, err := h.loadState()
	if err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update rollback checkpoint is unavailable")
	}
	preparedAuthority := state.preparedRollbackMatches(envelope.OperationID, request.Release.ManifestDigest, request.RollbackRef)
	rollbackRelease, rollbackSequence, rollbackDigest := state.RollbackRelease, state.RollbackSequence, state.RollbackDigest
	if preparedAuthority {
		if !state.hasBoundPreparedRollback() {
			return nil, broker.Failure(broker.CodeExecution, "update rollback checkpoint is unavailable")
		}
		rollbackRelease, rollbackSequence, rollbackDigest = state.PreparedRollbackRelease, state.PreparedRollbackSequence, state.PreparedRollbackDigest
	} else if !state.rollbackMatches(envelope.OperationID, request.Release.ManifestDigest, request.RollbackRef) {
		return nil, broker.Failure(broker.CodeFence, "update rollback checkpoint binding changed")
	}
	stored, identityErr := loadReleaseIdentity(filepath.Join(h.ReleaseRoot, releaseDirectoryName(request.Release)))
	if identityErr != nil || !sameReleaseIdentity(stored, request.Release, false) {
		return nil, broker.Failure(broker.CodeRevision, "rollback release metadata changed")
	}
	rollbackRoot := filepath.Join(h.ReleaseRoot, rollbackRelease)
	previousRelease := state.ActiveRelease
	rollbackProfile, profileErr := releaseBinaryProfile(rollbackRoot)
	if err := h.verifyReleaseExecutables(rollbackRoot); err != nil || h.activateLink(rollbackRelease) != nil || h.rewriteClientManifest(rollbackRoot) != nil ||
		profileErr != nil || h.OwnerManifest == nil || h.OwnerManifest(ctx, rollbackRoot, rollbackProfile) != nil {
		h.restoreActivation(ctx, previousRelease)
		return nil, broker.Failure(broker.CodeExecution, "update rollback could not be committed")
	}
	state.ActiveRelease, state.ActiveSequence, state.ActiveDigest = rollbackRelease, rollbackSequence, rollbackDigest
	if preparedAuthority {
		state.clearPreparedRollback()
	} else {
		state.clearRollback()
	}
	state.VerifiedSequence, state.VerifiedDigest, state.UpdatedAt = 0, "", h.Now().Unix()
	if err := h.saveState(state); err != nil {
		if !updateAuthorityVisible(err) {
			h.restoreActivation(ctx, previousRelease)
		}
		return nil, broker.Failure(broker.CodeExecution, "rollback release state could not be committed")
	}
	h.pruneReleases(state.ActiveRelease, state.RollbackRelease)
	if h.Restart != nil {
		h.Restart(peer.PID)
	}
	return RollbackResultV1{ProviderRevision: ProviderRevision, RolledBack: true, ActiveSequence: state.ActiveSequence,
		ActiveDigest: state.ActiveDigest, ManagementReady: true}, nil
}

func (h *Host) operationInbox(operationID string, release ReleaseIdentityV1, create bool) (string, error) {
	if !safeOperationID(operationID) {
		return "", errors.New("invalid operation")
	}
	root := filepath.Join(h.InboxRoot, operationID)
	if !strings.HasPrefix(root, filepath.Clean(h.InboxRoot)+string(os.PathSeparator)) {
		return "", errors.New("invalid operation root")
	}
	if create {
		if err := h.inboxOps.ensureDirectory(h.InboxRoot, 0o700); err != nil {
			return "", err
		}
		created := false
		if _, err := h.inboxOps.lstat(root); errors.Is(err, os.ErrNotExist) {
			created = true
		} else if err != nil {
			return "", err
		}
		if err := h.inboxOps.ensureDirectory(root, 0o700); err != nil {
			return "", err
		}
		metadata := inboxMetadataV1{Schema: 1, OperationID: operationID, ReleaseID: release.ReleaseID,
			ManifestDigest: release.ManifestDigest, ArtifactSetDigest: release.ArtifactSetDigest, CreatedAt: h.Now().Unix()}
		if err := h.ensureInboxMetadata(root, metadata); err != nil {
			if created {
				_ = h.inboxOps.removeAll(root)
				_ = h.inboxOps.syncDirectory(h.InboxRoot)
			}
			return "", err
		}
	}
	metadata, err := h.loadInboxMetadata(root)
	if err != nil || metadata.OperationID != operationID || metadata.ReleaseID != release.ReleaseID ||
		metadata.ManifestDigest != release.ManifestDigest || metadata.ArtifactSetDigest != release.ArtifactSetDigest {
		return "", errors.Join(err, errors.New("update inbox binding changed"))
	}
	return root, nil
}

type inboxMetadataV1 struct {
	Schema            int    `json:"schema"`
	OperationID       string `json:"operationId"`
	ReleaseID         string `json:"releaseId"`
	ManifestDigest    string `json:"manifestDigest"`
	ArtifactSetDigest string `json:"artifactSetDigest"`
	CreatedAt         int64  `json:"createdAt"`
}

type updateStageFile interface {
	Stat() (os.FileInfo, error)
	ReadAt([]byte, int64) (int, error)
	WriteAt([]byte, int64) (int, error)
	Sync() error
	Close() error
}

type updateInboxOps struct {
	ensureDirectory func(string, os.FileMode) error
	lstat           func(string) (os.FileInfo, error)
	openFile        func(string, int, os.FileMode) (updateStageFile, error)
	readDir         func(string) ([]os.DirEntry, error)
	removeAll       func(string) error
	syncDirectory   func(string) error
}

var productionUpdateInboxOps = updateInboxOps{
	ensureDirectory: ensureUpdateDirectory,
	lstat:           os.Lstat,
	openFile: func(path string, flags int, mode os.FileMode) (updateStageFile, error) {
		return os.OpenFile(path, flags, mode)
	},
	readDir:       os.ReadDir,
	removeAll:     os.RemoveAll,
	syncDirectory: syncDirectory,
}

func (h *Host) ensureInboxMetadata(root string, expected inboxMetadataV1) error {
	path := filepath.Join(root, ".inbox.json")
	if _, err := h.inboxOps.lstat(path); err == nil {
		stored, loadErr := h.loadInboxMetadata(root)
		if loadErr != nil || stored.OperationID != expected.OperationID || stored.ReleaseID != expected.ReleaseID ||
			stored.ManifestDigest != expected.ManifestDigest || stored.ArtifactSetDigest != expected.ArtifactSetDigest {
			return errors.Join(loadErr, errors.New("update inbox metadata changed"))
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.Marshal(expected)
	if err != nil {
		return err
	}
	return publishUpdateAuthority(h.atomicOps, path, data, 0o600, 0, 0, func() error {
		stored, loadErr := h.loadInboxMetadata(root)
		if loadErr != nil || stored != expected {
			return errors.Join(loadErr, errors.New("published update inbox metadata did not reopen exactly"))
		}
		return nil
	})
}

func (h *Host) loadInboxMetadata(root string) (inboxMetadataV1, error) {
	path := filepath.Join(root, ".inbox.json")
	info, err := h.inboxOps.lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 || info.Size() > 16<<10 {
		return inboxMetadataV1{}, errors.Join(err, errors.New("update inbox metadata is unsafe"))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return inboxMetadataV1{}, err
	}
	var metadata inboxMetadataV1
	if err := decodeStrict(data, &metadata); err != nil || metadata.Schema != 1 || !safeOperationID(metadata.OperationID) ||
		!safeIDPattern.MatchString(metadata.ReleaseID) || !validDigest(metadata.ManifestDigest) || !validDigest(metadata.ArtifactSetDigest) || metadata.CreatedAt <= 0 {
		return inboxMetadataV1{}, errors.New("update inbox metadata is invalid")
	}
	return metadata, nil
}

func (h *Host) releaseStaging(_ context.Context, envelope broker.Request, _ broker.PeerIdentity) (any, error) {
	var request ReleaseStagingRequestV1
	if err := broker.DecodeRawPayload(envelope.Payload, &request); err != nil || !safeOperationID(envelope.OperationID) || !validDigest(request.ManifestDigest) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "update staging release request is invalid")
	}
	root := filepath.Join(h.InboxRoot, envelope.OperationID)
	if _, err := h.inboxOps.lstat(root); errors.Is(err, os.ErrNotExist) {
		return ReleaseStagingResultV1{ProviderRevision: ProviderRevision, Released: true}, nil
	} else if err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update staging release target is unavailable")
	}
	metadata, err := h.loadInboxMetadata(root)
	if err != nil || metadata.OperationID != envelope.OperationID || metadata.ManifestDigest != request.ManifestDigest {
		return nil, broker.Failure(broker.CodeFence, "update staging release binding changed")
	}
	if err := h.removeOwnedInbox(root); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update staging release could not be committed")
	}
	return ReleaseStagingResultV1{ProviderRevision: ProviderRevision, Released: true}, nil
}

func (h *Host) removeOwnedInbox(root string) error {
	if filepath.Dir(root) != filepath.Clean(h.InboxRoot) || !safeOperationID(filepath.Base(root)) || !pathWithin(h.InboxRoot, root) {
		return errors.New("update inbox removal target is unsafe")
	}
	if err := h.inboxOps.removeAll(root); err != nil {
		return err
	}
	return h.inboxOps.syncDirectory(h.InboxRoot)
}

type inboxRetentionCandidate struct {
	root      string
	operation string
	bytes     int64
	updatedAt time.Time
}

func (h *Host) reconcileStagingState(preserveOperation string) error {
	if err := h.sweepOperationInboxes(preserveOperation); err != nil {
		return err
	}
	return h.sweepIncompleteGenerations()
}

func (h *Host) sweepOperationInboxes(preserveOperation string) error {
	entries, err := h.inboxOps.readDir(h.InboxRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	candidates := make([]inboxRetentionCandidate, 0, len(entries))
	for _, entry := range entries {
		operationID := entry.Name()
		root := filepath.Join(h.InboxRoot, operationID)
		if !safeOperationID(operationID) || !entry.IsDir() || !pathWithin(h.InboxRoot, root) {
			continue
		}
		metadata, loadErr := h.loadInboxMetadata(root)
		if loadErr != nil || metadata.OperationID != operationID {
			// An exact name without valid owner metadata is foreign/ambiguous and
			// is never deleted by the Update owner.
			continue
		}
		bytes, updatedAt, sizeErr := ownedTreeUsage(root, maxResumableInboxBytes+1)
		if sizeErr != nil {
			return sizeErr
		}
		candidates = append(candidates, inboxRetentionCandidate{root: root, operation: operationID, bytes: bytes, updatedAt: updatedAt})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].operation == preserveOperation || candidates[j].operation == preserveOperation {
			return candidates[i].operation == preserveOperation
		}
		if candidates[i].updatedAt.Equal(candidates[j].updatedAt) {
			return candidates[i].operation > candidates[j].operation
		}
		return candidates[i].updatedAt.After(candidates[j].updatedAt)
	})
	now := h.Now()
	retainedCount, retainedBytes := 0, int64(0)
	for _, candidate := range candidates {
		preserve := candidate.operation == preserveOperation
		withinAge := !candidate.updatedAt.IsZero() && !candidate.updatedAt.After(now) && now.Sub(candidate.updatedAt) <= maxResumableInboxAge
		withinBounds := retainedCount < maxResumableInboxCount && candidate.bytes <= maxResumableInboxBytes-retainedBytes
		if preserve || withinAge && withinBounds {
			retainedCount++
			retainedBytes += candidate.bytes
			if retainedCount > maxResumableInboxCount || retainedBytes > maxResumableInboxBytes {
				return errors.New("preserved update inbox exceeds its retention bound")
			}
			continue
		}
		if err := h.removeOwnedInbox(candidate.root); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) sweepIncompleteGenerations() error {
	entries, err := h.generationOps.readDir(h.ReleaseRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	removed := false
	for _, entry := range entries {
		name := entry.Name()
		if !stagingDirectoryPattern.MatchString(name) {
			continue
		}
		path := filepath.Join(h.ReleaseRoot, name)
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !pathWithin(h.ReleaseRoot, path) {
			return errors.New("owned release staging entry is unsafe")
		}
		if err := h.generationOps.removeAll(path); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		return h.generationOps.syncDirectory(h.ReleaseRoot)
	}
	return nil
}

func ownedTreeUsage(root string, limit int64) (int64, time.Time, error) {
	total := int64(0)
	var newest time.Time
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !pathWithin(root, path) || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() {
			return errors.New("owned update inbox contains an unsafe entry")
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		if info.Mode().IsRegular() {
			if info.Size() < 0 {
				return errors.New("owned update inbox has an invalid file size")
			}
			if info.Size() >= limit || total > limit-info.Size() {
				total = limit
			} else {
				total += info.Size()
			}
		}
		return nil
	})
	return total, newest, err
}

func (h *Host) activateLink(releaseName string) error {
	if filepath.Base(releaseName) != releaseName {
		return errors.New("invalid release directory")
	}
	if err := os.MkdirAll(h.ReleaseRoot, 0o755); err != nil {
		return err
	}
	temporary := filepath.Join(h.ReleaseRoot, ".current-"+releaseName)
	_ = os.Remove(temporary)
	if err := os.Symlink(releaseName, temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, filepath.Join(h.ReleaseRoot, "current")); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return syncDirectory(h.ReleaseRoot)
}

func (h *Host) restoreActivation(_ context.Context, releaseName string) {
	if !releaseDirectoryPattern.MatchString(releaseName) {
		return
	}
	cleanupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	root := filepath.Join(h.ReleaseRoot, releaseName)
	profile, profileErr := releaseBinaryProfile(root)
	if h.verifyReleaseExecutables(root) != nil || profileErr != nil || h.activateLink(releaseName) != nil || h.rewriteClientManifest(root) != nil || h.OwnerManifest == nil {
		return
	}
	_ = h.OwnerManifest(cleanupContext, root, profile)
}

func writeOwnerManifest(ctx context.Context, releaseRoot, profile string) error {
	if profile == "core" {
		return removeUpdateAuthority(deploymentidentity.InstalledContractPath, productionUpdateRemovalOps)
	}
	if profile != "full" {
		return errors.New("release binary profile is invalid")
	}
	writers := []string{filepath.Join(releaseRoot, "solovey-owner-manifest")}
	if executable, err := os.Readlink("/proc/self/exe"); err == nil && filepath.IsAbs(executable) {
		writers = append(writers, filepath.Join(filepath.Dir(executable), "solovey-owner-manifest"))
	}
	for _, writer := range writers {
		object, err := executableobject.Open(writer, executableobject.Policy{MaxBytes: 256 << 20, RequireRegular: true, RequireExecutable: true,
			RequireRootOwner: true, ForbiddenMode: 0o022, RequireTrustedAncestry: true, AncestryOwner: 0, AncestryForbiddenMode: 0o022})
		if err != nil {
			continue
		}
		command := exec.CommandContext(ctx, object.ExecPath(0))
		command.ExtraFiles = []*os.File{object.File()}
		command.Env = []string{"LANG=C", "LC_ALL=C"}
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		runErr := command.Run()
		closeErr := object.Close()
		return errors.Join(runErr, closeErr)
	}
	return errors.New("owner manifest writer is unavailable")
}

func (h *Host) pruneReleases(keep ...string) {
	retained := make(map[string]bool, len(keep))
	for _, name := range keep {
		if releaseDirectoryPattern.MatchString(name) {
			retained[name] = true
		}
	}
	entries, err := os.ReadDir(h.ReleaseRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if retained[name] || !releaseDirectoryPattern.MatchString(name) {
			continue
		}
		path := filepath.Join(h.ReleaseRoot, name)
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !pathWithin(h.ReleaseRoot, path) {
			continue
		}
		_ = os.RemoveAll(path)
	}
}

func (h *Host) loadState() (diskState, error) {
	data, err := os.ReadFile(h.StatePath)
	if errors.Is(err, os.ErrNotExist) {
		return diskState{}, nil
	}
	if err != nil || len(data) == 0 || len(data) > 64<<10 {
		return diskState{}, errors.New("invalid update state")
	}
	var state diskState
	if err := decodeStrict(data, &state); err != nil {
		return diskState{}, err
	}
	if err := validateDiskState(state); err != nil {
		return diskState{}, err
	}
	return state, nil
}

func (h *Host) saveState(state diskState) error {
	if err := validateDiskState(state); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.StatePath), 0o700); err != nil {
		return err
	}
	return publishUpdateAuthority(h.atomicOps, h.StatePath, data, 0o600, 0, 0, func() error {
		reopened, err := h.loadState()
		if err != nil || reopened != state {
			return errors.Join(err, errors.New("published update state did not reopen exactly"))
		}
		return nil
	})
}

func (h *Host) persistState(state diskState) error {
	if h.saveStateFn != nil {
		return h.saveStateFn(state)
	}
	return h.saveState(state)
}

func (h *Host) runningRelease(releaseName string, peer broker.PeerIdentity) bool {
	if releaseName == "" || peer.Executable == "" {
		return false
	}
	releaseRoot := filepath.Join(h.ReleaseRoot, releaseName)
	self, err := os.Readlink("/proc/self/exe")
	return err == nil && pathWithin(releaseRoot, self) && pathWithin(releaseRoot, peer.Executable)
}

func (h *Host) rewriteClientManifest(releaseRoot string) error {
	manifest, err := broker.LoadManifest(h.Manifest)
	if err != nil {
		return err
	}
	for index := range manifest.Clients {
		entry := &manifest.Clients[index]
		binary := "solovey-ui"
		for _, role := range entry.Roles {
			if role == broker.RoleSSHProof {
				binary = "solovey-ssh-proof"
			}
		}
		path := filepath.Join(releaseRoot, binary)
		object, err := openDeployedClient(path, *entry)
		if err != nil {
			return err
		}
		identity := object.Identity()
		if err := object.Close(); err != nil {
			return err
		}
		entry.Executable, entry.ExecutableDigest, entry.Device, entry.Inode = path, identity.Digest, identity.Device, identity.Inode
	}
	manifest, err = broker.FinalizeManifest(manifest)
	if err != nil {
		return err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return publishUpdateAuthority(h.atomicOps, h.Manifest, append(data, '\n'), 0o640, 0, 0, func() error {
		reopened, err := broker.LoadManifest(h.Manifest)
		if err != nil || reopened.Revision != manifest.Revision {
			return errors.Join(err, errors.New("published broker client manifest did not reopen exactly"))
		}
		return nil
	})
}

type extractedReleaseMember struct {
	Relative string
	Size     int64
	Digest   string
	Mode     os.FileMode
}

type updateGenerationOps struct {
	ensureRoot    func(string, os.FileMode) error
	removeAll     func(string) error
	mkdir         func(string, os.FileMode) error
	readDir       func(string) ([]os.DirEntry, error)
	extract       func(string, string) ([]extractedReleaseMember, error)
	verifyProfile func(string, string) error
	syncTree      func(string) error
	rename        func(string, string) error
	syncDirectory func(string) error
	loadIdentity  func(string) (ReleaseIdentityV1, error)
	verifyTree    func(string) error
	verifyMembers func(string, []extractedReleaseMember) error
}

var productionUpdateGenerationOps = updateGenerationOps{
	ensureRoot:    ensureUpdateDirectory,
	removeAll:     os.RemoveAll,
	mkdir:         os.Mkdir,
	readDir:       os.ReadDir,
	extract:       extractRelease,
	verifyProfile: verifyPreparedReleaseExecutables,
	syncTree:      syncReleaseTree,
	rename:        os.Rename,
	syncDirectory: syncDirectory,
	loadIdentity:  loadReleaseIdentity,
	verifyTree:    verifyReleaseTree,
	verifyMembers: verifyExtractedReleaseMembers,
}

func (h *Host) publishPreparedGeneration(archive, finalRoot string, identity ReleaseIdentityV1) error {
	ops := h.generationOps
	if ops.ensureRoot == nil || ops.removeAll == nil || ops.mkdir == nil || ops.readDir == nil || ops.extract == nil || ops.verifyProfile == nil ||
		ops.syncTree == nil || ops.rename == nil || ops.syncDirectory == nil || ops.loadIdentity == nil || ops.verifyTree == nil || ops.verifyMembers == nil {
		return errors.New("update generation filesystem contract is unavailable")
	}
	if !pathWithin(h.ReleaseRoot, finalRoot) || filepath.Dir(finalRoot) != filepath.Clean(h.ReleaseRoot) || !releaseDirectoryPattern.MatchString(filepath.Base(finalRoot)) {
		return errors.New("prepared release path is invalid")
	}
	if err := ops.ensureRoot(h.ReleaseRoot, 0o755); err != nil {
		return err
	}
	staging := finalRoot + ".staging"
	if err := ops.removeAll(staging); err != nil {
		return err
	}
	if err := ops.mkdir(staging, 0o755); err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			_ = ops.removeAll(staging)
		}
	}()
	members, err := ops.extract(archive, staging)
	if err != nil {
		return err
	}
	if err := ops.verifyProfile(staging, identity.BinaryProfile); err != nil {
		return err
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	identityPath := filepath.Join(staging, "release-identity.json")
	if err := publishUpdateAuthority(h.atomicOps, identityPath, encoded, 0o600, 0, 0, func() error {
		stored, loadErr := loadReleaseIdentity(staging)
		if loadErr != nil || !sameReleaseIdentity(stored, identity, true) {
			return errors.Join(loadErr, errors.New("staged release identity did not reopen exactly"))
		}
		return nil
	}); err != nil {
		return err
	}
	if err := ops.syncTree(staging); err != nil {
		return err
	}
	if err := ops.rename(staging, finalRoot); err != nil {
		return err
	}
	published = true
	if err := ops.syncDirectory(h.ReleaseRoot); err != nil {
		return &updateAuthorityPublishedError{cause: err}
	}
	stored, err := ops.loadIdentity(finalRoot)
	if err != nil || !sameReleaseIdentity(stored, identity, true) {
		return &updateAuthorityPublishedError{cause: errors.Join(err, errors.New("published release identity did not reopen exactly"))}
	}
	if err := ops.verifyProfile(finalRoot, identity.BinaryProfile); err != nil {
		return &updateAuthorityPublishedError{cause: err}
	}
	if err := ops.verifyTree(finalRoot); err != nil {
		return &updateAuthorityPublishedError{cause: err}
	}
	if err := ops.verifyMembers(finalRoot, members); err != nil {
		return &updateAuthorityPublishedError{cause: err}
	}
	return nil
}

func extractRelease(archive, destination string) ([]extractedReleaseMember, error) {
	file, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	count, total := 0, int64(0)
	members := make([]extractedReleaseMember, 0, 16)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return members, nil
		}
		if err != nil {
			return nil, err
		}
		name := filepath.ToSlash(header.Name)
		prefix := "solovey-ui/"
		if !strings.HasPrefix(name, prefix) {
			return nil, errors.New("release archive root is invalid")
		}
		relative := strings.TrimPrefix(name, prefix)
		if relative == "" {
			continue
		}
		if !allowedReleasePath(relative) || header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > 512<<20 {
			return nil, errors.New("release archive member is invalid")
		}
		count++
		total += header.Size
		if count > 64 || total > 1<<30 {
			return nil, errors.New("release archive exceeds bounds")
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		if !pathWithin(destination, target) {
			return nil, errors.New("release archive path escaped")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		mode := os.FileMode(0o644)
		if executableReleasePath(relative) {
			mode = 0o755
		}
		member, err := writeExtractedReleaseMember(productionReleaseMemberOps, target, relative, reader, header.Size, mode)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
}

type releaseMemberFile interface {
	io.Writer
	Sync() error
	Close() error
}

type releaseMemberOps struct {
	openFile func(string, int, os.FileMode) (releaseMemberFile, error)
}

var productionReleaseMemberOps = releaseMemberOps{
	openFile: func(path string, flags int, mode os.FileMode) (releaseMemberFile, error) {
		return os.OpenFile(path, flags, mode)
	},
}

func writeExtractedReleaseMember(ops releaseMemberOps, target, relative string, source io.Reader, size int64, mode os.FileMode) (extractedReleaseMember, error) {
	if ops.openFile == nil || size < 0 || !allowedReleasePath(filepath.ToSlash(relative)) {
		return extractedReleaseMember{}, errors.New("release member writer contract is unavailable")
	}
	output, err := ops.openFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return extractedReleaseMember{}, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = output.Close()
		}
	}()
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hash), io.LimitReader(source, size+1))
	syncErr := output.Sync()
	closeErr := output.Close()
	closed = true
	if copyErr != nil || syncErr != nil || closeErr != nil || written != size {
		return extractedReleaseMember{}, errors.Join(copyErr, syncErr, closeErr, errors.New("release member size mismatch"))
	}
	return extractedReleaseMember{Relative: filepath.ToSlash(relative), Size: written, Digest: hex.EncodeToString(hash.Sum(nil)), Mode: mode.Perm()}, nil
}

func ensureUpdateDirectory(path string, mode os.FileMode) error {
	path = filepath.Clean(path)
	created := false
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, mode); err != nil {
			return err
		}
		created = true
	} else if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode.Perm() {
		return errors.New("update directory is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return errors.New("update directory is not root-owned")
	}
	if created {
		return syncDirectory(filepath.Dir(path))
	}
	return nil
}

func syncReleaseTree(root string) error {
	directories := make([]string, 0, 8)
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !pathWithin(root, path) || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("release tree contains an unsafe entry")
		}
		if info.IsDir() {
			directories = append(directories, path)
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("release tree contains a non-regular member")
		}
		return nil
	})
	if err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := syncDirectory(directories[index]); err != nil {
			return err
		}
	}
	return nil
}

func verifyReleaseTree(root string) error {
	files, directories := 0, 0
	total := int64(0)
	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !pathWithin(root, path) || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("release tree contains an unsafe entry")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode().Perm()&0o022 != 0 {
			return errors.New("release tree entry ownership or mode is unsafe")
		}
		if info.IsDir() {
			directories++
			if directories > 16 {
				return errors.New("release tree contains too many directories")
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("release tree contains a non-regular member")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative != "release-identity.json" && !allowedReleasePath(relative) {
			return errors.New("release tree contains an unknown member")
		}
		files++
		total += info.Size()
		if files > 65 || info.Size() < 0 || info.Size() > 512<<20 || total > 1<<30+(64<<10) {
			return errors.New("release tree exceeds its bound")
		}
		return nil
	})
}

func verifyExtractedReleaseMembers(root string, members []extractedReleaseMember) error {
	if len(members) == 0 || len(members) > 64 {
		return errors.New("release member identity set is invalid")
	}
	seen := make(map[string]bool, len(members))
	for _, member := range members {
		if seen[member.Relative] || !allowedReleasePath(member.Relative) {
			return errors.New("release member identity is invalid")
		}
		seen[member.Relative] = true
		path := filepath.Join(root, filepath.FromSlash(member.Relative))
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != member.Mode.Perm() || info.Size() != member.Size {
			return errors.Join(err, errors.New("published release member changed"))
		}
		digest, err := fileDigest(path)
		if err != nil || digest != member.Digest {
			return errors.Join(err, errors.New("published release member digest changed"))
		}
	}
	return nil
}

type updateAtomicFile interface {
	Name() string
	Chmod(os.FileMode) error
	Chown(int, int) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type updateAtomicOps struct {
	createTemp    func(string, string) (updateAtomicFile, error)
	remove        func(string) error
	rename        func(string, string) error
	syncDirectory func(string) error
}

var productionUpdateAtomicOps = updateAtomicOps{
	createTemp: func(directory, pattern string) (updateAtomicFile, error) {
		return os.CreateTemp(directory, pattern)
	},
	remove:        os.Remove,
	rename:        os.Rename,
	syncDirectory: syncDirectory,
}

type updateAuthorityPublishedError struct {
	cause error
}

func (e *updateAuthorityPublishedError) Error() string {
	return "update authority is visible but its publication boundary failed: " + e.cause.Error()
}

func (e *updateAuthorityPublishedError) Unwrap() error { return e.cause }

func updateAuthorityVisible(err error) bool {
	var published *updateAuthorityPublishedError
	return errors.As(err, &published)
}

func publishUpdateAuthority(ops updateAtomicOps, path string, data []byte, mode os.FileMode, uid, gid int, reopen func() error) error {
	if ops.createTemp == nil || ops.remove == nil || ops.rename == nil || ops.syncDirectory == nil || reopen == nil {
		return errors.New("update atomic publication contract is unavailable")
	}
	directory := filepath.Dir(path)
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("update authority directory is unsafe")
	}
	temporary, err := ops.createTemp(directory, ".solovey-update-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	closed, published := false, false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		if !published {
			_ = ops.remove(name)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if err := temporary.Chown(uid, gid); err != nil {
		return err
	}
	written, err := temporary.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		closed = true
		return err
	}
	closed = true
	if err := ops.rename(name, path); err != nil {
		return err
	}
	published = true
	if err := ops.syncDirectory(directory); err != nil {
		return &updateAuthorityPublishedError{cause: err}
	}
	if err := reopen(); err != nil {
		return &updateAuthorityPublishedError{cause: err}
	}
	return nil
}

type updateRemovalOps struct {
	remove        func(string) error
	syncDirectory func(string) error
}

var productionUpdateRemovalOps = updateRemovalOps{remove: os.Remove, syncDirectory: syncDirectory}

func removeUpdateAuthority(path string, ops updateRemovalOps) error {
	if ops.remove == nil || ops.syncDirectory == nil {
		return errors.New("update authority removal contract is unavailable")
	}
	if err := ops.remove(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := ops.syncDirectory(filepath.Dir(path)); err != nil {
		return &updateAuthorityPublishedError{cause: err}
	}
	return nil
}

func allowedReleasePath(value string) bool {
	if strings.Contains(value, "..") || strings.Contains(value, "\\") {
		return false
	}
	if strings.HasPrefix(value, "systemd/") {
		return safeNamePattern.MatchString(strings.TrimPrefix(value, "systemd/"))
	}
	return safeNamePattern.MatchString(value)
}

func executableReleasePath(value string) bool {
	return value == "solovey-ui" || value == "solovey-privileged-broker" || value == "solovey-ssh-proof" ||
		value == "solovey-broker-manifest" || value == "solovey-owner-manifest" || value == "solovey-ui.sh"
}

func verifyReleaseExecutables(root string) error {
	for _, name := range []string{"solovey-ui", "solovey-privileged-broker", "solovey-ssh-proof", "solovey-broker-manifest"} {
		if !safeReleaseExecutable(filepath.Join(root, name)) {
			return fmt.Errorf("release executable %s is unsafe", name)
		}
	}
	return nil
}

func verifyPreparedReleaseExecutables(root, profile string) error {
	if err := verifyReleaseExecutables(root); err != nil {
		return err
	}
	return verifyReleaseOwnerWriter(root, profile)
}

func verifyReleaseOwnerWriter(root, profile string) error {
	ownerWriter := filepath.Join(root, "solovey-owner-manifest")
	if profile == "core" {
		if _, err := os.Lstat(ownerWriter); !errors.Is(err, os.ErrNotExist) {
			return errors.New("core release contains application owner manifest writer")
		}
		return nil
	}
	if profile != "full" || !safeReleaseExecutable(ownerWriter) {
		return errors.New("release executable solovey-owner-manifest is unsafe")
	}
	return nil
}

func releaseBinaryProfile(root string) (string, error) {
	identityPath := filepath.Join(root, "release-identity.json")
	if _, statErr := os.Lstat(identityPath); statErr == nil {
		identity, err := loadReleaseIdentity(root)
		if err != nil {
			return "", err
		}
		if identity.BinaryProfile == "full" || identity.BinaryProfile == "core" {
			return identity.BinaryProfile, nil
		}
		return "", errors.New("release binary profile is invalid")
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	data, err := os.ReadFile(filepath.Join(root, "BUILD_INFO.txt"))
	if err != nil || len(data) == 0 || len(data) > 64<<10 {
		return "", errors.New("release build metadata is unavailable")
	}
	profile := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "profile=") {
			if profile != "" {
				return "", errors.New("release build metadata repeats profile")
			}
			profile = strings.TrimSpace(strings.TrimPrefix(line, "profile="))
		}
	}
	if profile != "full" && profile != "core" {
		return "", errors.New("release build metadata profile is invalid")
	}
	return profile, nil
}

func safeReleaseExecutable(name string) bool {
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || info.Mode().Perm()&0o111 == 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0 && stat.Gid == 0 && info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0
}

func loadReleaseIdentity(root string) (ReleaseIdentityV1, error) {
	path := filepath.Join(root, "release-identity.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 || info.Size() > 64<<10 {
		return ReleaseIdentityV1{}, errors.New("release identity file is unsafe")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ReleaseIdentityV1{}, err
	}
	var identity ReleaseIdentityV1
	if err := decodeStrict(data, &identity); err != nil || len(identity.Artifacts) == 0 || ValidateRelease(identity) != nil {
		return ReleaseIdentityV1{}, errors.New("release identity file is invalid")
	}
	return identity, nil
}

func sameReleaseIdentity(stored, requested ReleaseIdentityV1, requireArtifacts bool) bool {
	if stored.ReleaseID != requested.ReleaseID || stored.Sequence != requested.Sequence || stored.Version != requested.Version ||
		stored.ManifestDigest != requested.ManifestDigest ||
		stored.ArtifactSetDigest != requested.ArtifactSetDigest || stored.BinaryProfile != requested.BinaryProfile ||
		stored.DeploymentRevision != requested.DeploymentRevision || stored.MigrationSetDigest != requested.MigrationSetDigest ||
		stored.RestartClass != requested.RestartClass || stored.RollbackClass != requested.RollbackClass {
		return false
	}
	if !requireArtifacts {
		return true
	}
	left, _ := json.Marshal(stored.Artifacts)
	right, _ := json.Marshal(requested.Artifacts)
	return string(left) == string(right)
}

func (state diskState) activationHighWater() (uint64, string) {
	sequence, digest := state.MaxActivatedSequence, state.MaxActivatedDigest
	if state.ActiveSequence > sequence {
		sequence, digest = state.ActiveSequence, state.ActiveDigest
	}
	if state.VerifiedSequence > sequence {
		sequence, digest = state.VerifiedSequence, state.VerifiedDigest
	}
	return sequence, digest
}

func (state *diskState) raiseHighWater(sequence uint64, digest string) {
	if sequence > state.MaxActivatedSequence {
		state.MaxActivatedSequence, state.MaxActivatedDigest = sequence, digest
	}
}

func (state diskState) hasBoundRollback() bool {
	return state.RollbackRelease != "" && state.RollbackSequence > 0 && validDigest(state.RollbackDigest) &&
		safeOperationID(state.RollbackOperationID) && validDigest(state.RollbackManifestDigest) &&
		state.RollbackRef == semanticRef("rollback", state.RollbackOperationID, state.RollbackManifestDigest)
}

func (state diskState) rollbackMatches(operationID, manifestDigest, rollbackRef string) bool {
	return state.hasBoundRollback() && state.RollbackOperationID == operationID &&
		state.RollbackManifestDigest == manifestDigest && state.RollbackRef == rollbackRef
}

func (state diskState) hasBoundPreparedRollback() bool {
	return state.hasPreparedRollbackBinding() && state.PreparedRollbackRelease != "" &&
		state.PreparedRollbackSequence > 0 && validDigest(state.PreparedRollbackDigest)
}

func (state diskState) hasPreparedRollbackBinding() bool {
	return safeOperationID(state.PreparedRollbackOperationID) && validDigest(state.PreparedRollbackManifestDigest) &&
		state.PreparedRollbackRef == semanticRef("rollback", state.PreparedRollbackOperationID, state.PreparedRollbackManifestDigest)
}

func (state diskState) preparedRollbackMatches(operationID, manifestDigest, rollbackRef string) bool {
	return state.hasPreparedRollbackBinding() && state.PreparedRollbackOperationID == operationID &&
		state.PreparedRollbackManifestDigest == manifestDigest && state.PreparedRollbackRef == rollbackRef
}

func (state *diskState) clearRollback() {
	state.RollbackRelease, state.RollbackSequence, state.RollbackDigest = "", 0, ""
	state.RollbackOperationID, state.RollbackManifestDigest, state.RollbackRef = "", "", ""
}

func (state *diskState) clearPreparedRollback() {
	state.PreparedRollbackRelease, state.PreparedRollbackSequence, state.PreparedRollbackDigest = "", 0, ""
	state.PreparedRollbackOperationID, state.PreparedRollbackManifestDigest, state.PreparedRollbackRef = "", "", ""
}

func (state *diskState) promotePreparedRollback() {
	if state.hasBoundPreparedRollback() {
		state.RollbackRelease, state.RollbackSequence, state.RollbackDigest = state.PreparedRollbackRelease, state.PreparedRollbackSequence, state.PreparedRollbackDigest
		state.RollbackOperationID, state.RollbackManifestDigest, state.RollbackRef = state.PreparedRollbackOperationID, state.PreparedRollbackManifestDigest, state.PreparedRollbackRef
	}
	state.clearPreparedRollback()
}

func validateDiskState(state diskState) error {
	validRelease := func(value string) bool {
		return value == "" || len(value) <= 128 && filepath.Base(value) == value && value != "." && value != "current" && !strings.HasSuffix(value, ".staging")
	}
	validSequence := func(sequence uint64, digest string) bool {
		return sequence == 0 && digest == "" || sequence > 0 && validDigest(digest)
	}
	legacyRollback := state.RollbackRelease != "" && state.RollbackOperationID == "" && state.RollbackManifestDigest == "" && state.RollbackRef == ""
	preparedPresent := state.PreparedRollbackRelease != "" || state.PreparedRollbackSequence != 0 || state.PreparedRollbackDigest != "" ||
		state.PreparedRollbackOperationID != "" || state.PreparedRollbackManifestDigest != "" || state.PreparedRollbackRef != ""
	if !validRelease(state.ActiveRelease) || !validRelease(state.RollbackRelease) || !validRelease(state.PreparedRollbackRelease) ||
		!validSequence(state.ActiveSequence, state.ActiveDigest) || !validSequence(state.VerifiedSequence, state.VerifiedDigest) ||
		!validSequence(state.RollbackSequence, state.RollbackDigest) || !validSequence(state.PreparedRollbackSequence, state.PreparedRollbackDigest) ||
		!validSequence(state.MaxActivatedSequence, state.MaxActivatedDigest) ||
		state.VerifiedSequence > 0 && (state.VerifiedSequence != state.ActiveSequence || state.VerifiedDigest != state.ActiveDigest) ||
		state.ActiveRelease == "" && state.ActiveSequence > 0 || state.RollbackRelease == "" && state.RollbackSequence > 0 ||
		state.PreparedRollbackRelease == "" && state.PreparedRollbackSequence > 0 ||
		state.RollbackRelease != "" && !legacyRollback && !state.hasBoundRollback() ||
		preparedPresent && !state.hasPreparedRollbackBinding() || state.PreparedRollbackRelease != "" && !state.hasBoundPreparedRollback() ||
		state.MaxActivatedSequence > 0 && state.MaxActivatedSequence < state.ActiveSequence ||
		state.MaxActivatedSequence == state.ActiveSequence && state.MaxActivatedSequence > 0 && state.MaxActivatedDigest != state.ActiveDigest {
		return errors.New("invalid update state")
	}
	return nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("multiple JSON values are forbidden")
	}
	return nil
}

func releaseDirectoryName(value ReleaseIdentityV1) string {
	return fmt.Sprintf("%020d-%s", value.Sequence, value.ManifestDigest[:16])
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, 1<<30+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func ensureRegularOrAbsent(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return errors.New("unsafe artifact")
	}
	return nil
}

func pathWithin(root, candidate string) bool {
	root, candidate = filepath.Clean(root), filepath.Clean(candidate)
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func scheduleReleaseRestart(panelPID int) {
	go func() {
		time.Sleep(1500 * time.Millisecond)
		if panelPID > 1 {
			_ = syscall.Kill(panelPID, syscall.SIGTERM)
		}
		time.Sleep(250 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	}()
}

func mustJSON(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}
