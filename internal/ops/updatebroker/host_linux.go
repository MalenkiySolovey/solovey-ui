//go:build linux

package updatebroker

import (
	"archive/tar"
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
	"strings"
	"syscall"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

var releaseDirectoryPattern = regexp.MustCompile(`^(?:[0-9]{20}|installer)-[a-f0-9]{16}$`)

const (
	defaultInboxRoot   = "/var/lib/solovey-ui-broker/update-inbox"
	defaultReleaseRoot = "/usr/local/solovey-ui/releases"
	defaultStatePath   = "/var/lib/solovey-ui-broker/update-state.json"
	defaultManifest    = "/etc/solovey-ui/broker-clients.json"
)

type diskState struct {
	ActiveSequence   uint64 `json:"activeSequence"`
	ActiveDigest     string `json:"activeDigest"`
	VerifiedSequence uint64 `json:"verifiedSequence"`
	VerifiedDigest   string `json:"verifiedDigest"`
	ActiveRelease    string `json:"activeRelease"`
	RollbackRelease  string `json:"rollbackRelease,omitempty"`
	RollbackSequence uint64 `json:"rollbackSequence,omitempty"`
	RollbackDigest   string `json:"rollbackDigest,omitempty"`
	UpdatedAt        int64  `json:"updatedAt"`
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
}

func NewHost() *Host {
	return &Host{InboxRoot: defaultInboxRoot, ReleaseRoot: defaultReleaseRoot, StatePath: defaultStatePath,
		Manifest: defaultManifest, Now: time.Now, Restart: scheduleReleaseRestart, OwnerManifest: writeOwnerManifest}
}

func RegisterHandlers(registry *broker.Registry) error {
	host := NewHost()
	definitions := []struct {
		verb     broker.Verb
		mutation bool
		handler  broker.Handler
	}{
		{broker.VerbUpdateObserve, false, host.observe},
		{broker.VerbUpdateStage, true, host.stage},
		{broker.VerbUpdatePrepare, true, host.prepare},
		{broker.VerbUpdateActivate, true, host.activate},
		{broker.VerbUpdateVerify, true, host.verify},
		{broker.VerbUpdateRollback, true, host.rollback},
	}
	for _, definition := range definitions {
		if err := registry.Register(definition.verb, broker.Definition{Role: broker.RolePanel, Mutation: definition.mutation, Handler: definition.handler}); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) observe(_ context.Context, _ broker.Request, peer broker.PeerIdentity) (any, error) {
	state, err := h.loadState()
	if err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update state is unavailable")
	}
	managementReady := peer.PID > 0
	verified := h.runningRelease(state.ActiveRelease, peer)
	if verified {
		state.VerifiedSequence, state.VerifiedDigest = state.ActiveSequence, state.ActiveDigest
		if err := h.persistState(state); err != nil {
			return nil, broker.Failure(broker.CodeExecution, "verified update state could not be persisted")
		}
	}
	result := ObservationV1{ProviderRevision: ProviderRevision, InstalledSequence: state.ActiveSequence,
		ActiveSequence: state.ActiveSequence, ActiveDigest: state.ActiveDigest, InstalledDigest: state.ActiveDigest,
		VerifiedSequence: state.VerifiedSequence, VerifiedDigest: state.VerifiedDigest,
		RollbackAvailable: state.RollbackRelease != "", ManagementReady: managementReady, ObservedAt: h.Now().Unix()}
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
	root, err := h.operationInbox(envelope.OperationID)
	if err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update inbox is unavailable")
	}
	path := filepath.Join(root, request.Artifact.Role+"-"+request.Artifact.Name)
	if err := ensureRegularOrAbsent(path); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update artifact target is unsafe")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update artifact could not be staged")
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() != request.Offset {
		_ = file.Close()
		return nil, broker.Failure(broker.CodeFence, "update artifact offset changed")
	}
	if _, err = file.WriteAt(request.Chunk, request.Offset); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return nil, broker.Failure(broker.CodeExecution, "update artifact chunk was not committed")
	}
	accepted := request.Offset + int64(len(request.Chunk))
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
	inbox, err := h.operationInbox(envelope.OperationID)
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
	if state.ActiveSequence > 0 && request.Release.Sequence <= state.ActiveSequence {
		return nil, broker.Failure(broker.CodeRevision, "prepared release sequence is not monotonic")
	}
	if info, err := os.Lstat(finalRoot); errors.Is(err, os.ErrNotExist) {
		staging := finalRoot + ".staging"
		if err := os.MkdirAll(h.ReleaseRoot, 0o755); err != nil {
			return nil, broker.Failure(broker.CodeExecution, "release root is unavailable")
		}
		_ = os.RemoveAll(staging)
		if err := os.Mkdir(staging, 0o755); err != nil {
			return nil, broker.Failure(broker.CodeExecution, "release staging root is unavailable")
		}
		if err := extractRelease(panelArtifact, staging); err != nil {
			_ = os.RemoveAll(staging)
			return nil, broker.Failure(broker.CodeExecution, "release payload is invalid")
		}
		if err := verifyPreparedReleaseExecutables(staging, request.Release.BinaryProfile); err != nil {
			_ = os.RemoveAll(staging)
			return nil, broker.Failure(broker.CodeExecution, "release payload is incomplete")
		}
		identity, _ := json.Marshal(request.Release)
		if err := os.WriteFile(filepath.Join(staging, "release-identity.json"), identity, 0o600); err != nil {
			_ = os.RemoveAll(staging)
			return nil, broker.Failure(broker.CodeExecution, "release identity could not be staged")
		}
		if err := os.Rename(staging, finalRoot); err != nil {
			_ = os.RemoveAll(staging)
			return nil, broker.Failure(broker.CodeExecution, "release staging could not be committed")
		}
	} else if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, broker.Failure(broker.CodeExecution, "prepared release target is unsafe")
	} else if stored, err := loadReleaseIdentity(finalRoot); err != nil || !sameReleaseIdentity(stored, request.Release, true) {
		return nil, broker.Failure(broker.CodeRevision, "prepared release identity changed")
	}
	if state.ActiveRelease == "" {
		if current, linkErr := os.Readlink(filepath.Join(h.ReleaseRoot, "current")); linkErr == nil {
			candidate := filepath.Base(current)
			if candidate == current && verifyReleaseExecutables(filepath.Join(h.ReleaseRoot, candidate)) == nil {
				state.ActiveRelease = candidate
			}
		}
	}
	state.RollbackRelease, state.RollbackSequence, state.RollbackDigest = state.ActiveRelease, state.ActiveSequence, state.ActiveDigest
	state.UpdatedAt = h.Now().Unix()
	if err := h.saveState(state); err != nil {
		return nil, broker.Failure(broker.CodeExecution, "release rollback checkpoint could not be committed")
	}
	_ = os.RemoveAll(inbox)
	return PrepareResultV1{ProviderRevision: ProviderRevision, PreparedRef: semanticRef("prepared", envelope.OperationID, request.Release.ManifestDigest),
		RollbackRef: semanticRef("rollback", envelope.OperationID, request.Release.ManifestDigest), ManagementReady: true,
		PreflightRevision: broker.Digest([]byte(releaseName + ":" + request.ExpectedManagementRevision))}, nil
}

func (h *Host) activate(ctx context.Context, envelope broker.Request, peer broker.PeerIdentity) (any, error) {
	var request ActivateRequestV1
	if err := broker.DecodeRawPayload(envelope.Payload, &request); err != nil || ValidateRelease(request.Release) != nil || request.ExpectedMode != "native" ||
		request.PreparedRef != semanticRef("prepared", envelope.OperationID, request.Release.ManifestDigest) ||
		request.RollbackRef != semanticRef("rollback", envelope.OperationID, request.Release.ManifestDigest) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "update activation request is invalid")
	}
	releaseName := releaseDirectoryName(request.Release)
	releaseRoot := filepath.Join(h.ReleaseRoot, releaseName)
	stored, identityErr := loadReleaseIdentity(releaseRoot)
	if err := verifyPreparedReleaseExecutables(releaseRoot, request.Release.BinaryProfile); err != nil || identityErr != nil || !sameReleaseIdentity(stored, request.Release, false) {
		return nil, broker.Failure(broker.CodeExecution, "prepared release is unavailable")
	}
	state, err := h.loadState()
	if err != nil {
		return nil, broker.Failure(broker.CodeExecution, "update state is unavailable")
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
	if err := h.saveState(state); err != nil {
		h.restoreActivation(ctx, previousRelease)
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
	if err := broker.DecodeRawPayload(envelope.Payload, &request); err != nil || ValidateRelease(request.Release) != nil ||
		request.PreparedRef != semanticRef("prepared", envelope.OperationID, request.Release.ManifestDigest) ||
		request.RollbackRef != semanticRef("rollback", envelope.OperationID, request.Release.ManifestDigest) {
		return nil, broker.Failure(broker.CodeInvalidRequest, "active release verification request is invalid")
	}
	state, err := h.loadState()
	if err != nil || state.ActiveSequence != request.Release.Sequence || state.ActiveDigest != request.Release.ManifestDigest {
		return nil, broker.Failure(broker.CodeRevision, "active release identity changed")
	}
	stored, identityErr := loadReleaseIdentity(filepath.Join(h.ReleaseRoot, state.ActiveRelease))
	if identityErr != nil || !sameReleaseIdentity(stored, request.Release, false) {
		return nil, broker.Failure(broker.CodeRevision, "active release metadata changed")
	}
	verified := h.runningRelease(state.ActiveRelease, peer)
	healthRevision := "restart-pending"
	if verified {
		state.VerifiedSequence, state.VerifiedDigest, state.UpdatedAt = state.ActiveSequence, state.ActiveDigest, h.Now().Unix()
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
	if err := broker.DecodeRawPayload(envelope.Payload, &request); err != nil || ValidateRelease(request.Release) != nil ||
		request.RollbackRef != semanticRef("rollback", envelope.OperationID, request.Release.ManifestDigest) || len(request.ReasonCode) > 96 {
		return nil, broker.Failure(broker.CodeInvalidRequest, "update rollback request is invalid")
	}
	state, err := h.loadState()
	if err != nil || state.RollbackRelease == "" {
		return nil, broker.Failure(broker.CodeExecution, "update rollback checkpoint is unavailable")
	}
	stored, identityErr := loadReleaseIdentity(filepath.Join(h.ReleaseRoot, releaseDirectoryName(request.Release)))
	if identityErr != nil || !sameReleaseIdentity(stored, request.Release, false) {
		return nil, broker.Failure(broker.CodeRevision, "rollback release metadata changed")
	}
	rollbackRoot := filepath.Join(h.ReleaseRoot, state.RollbackRelease)
	previousRelease := state.ActiveRelease
	rollbackProfile, profileErr := releaseBinaryProfile(rollbackRoot)
	if err := verifyReleaseExecutables(rollbackRoot); err != nil || h.activateLink(state.RollbackRelease) != nil || h.rewriteClientManifest(rollbackRoot) != nil ||
		profileErr != nil || h.OwnerManifest == nil || h.OwnerManifest(ctx, rollbackRoot, rollbackProfile) != nil {
		h.restoreActivation(ctx, previousRelease)
		return nil, broker.Failure(broker.CodeExecution, "update rollback could not be committed")
	}
	state.ActiveRelease, state.ActiveSequence, state.ActiveDigest = state.RollbackRelease, state.RollbackSequence, state.RollbackDigest
	state.RollbackRelease, state.RollbackSequence, state.RollbackDigest = "", 0, ""
	state.VerifiedSequence, state.VerifiedDigest, state.UpdatedAt = 0, "", h.Now().Unix()
	if err := h.saveState(state); err != nil {
		h.restoreActivation(ctx, previousRelease)
		return nil, broker.Failure(broker.CodeExecution, "rollback release state could not be committed")
	}
	h.pruneReleases(state.ActiveRelease)
	if h.Restart != nil {
		h.Restart(peer.PID)
	}
	return RollbackResultV1{ProviderRevision: ProviderRevision, RolledBack: true, ActiveSequence: state.ActiveSequence,
		ActiveDigest: state.ActiveDigest, ManagementReady: true}, nil
}

func (h *Host) operationInbox(operationID string) (string, error) {
	if !safeOperationID(operationID) {
		return "", errors.New("invalid operation")
	}
	if err := os.MkdirAll(h.InboxRoot, 0o700); err != nil {
		return "", err
	}
	root := filepath.Join(h.InboxRoot, operationID)
	if !strings.HasPrefix(root, filepath.Clean(h.InboxRoot)+string(os.PathSeparator)) {
		return "", errors.New("invalid operation root")
	}
	return root, os.MkdirAll(root, 0o700)
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
	if verifyReleaseExecutables(root) != nil || profileErr != nil || h.activateLink(releaseName) != nil || h.rewriteClientManifest(root) != nil || h.OwnerManifest == nil {
		return
	}
	_ = h.OwnerManifest(cleanupContext, root, profile)
}

func writeOwnerManifest(ctx context.Context, releaseRoot, profile string) error {
	if profile == "core" {
		if err := os.Remove(deploymentidentity.InstalledContractPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if profile != "full" {
		return errors.New("release binary profile is invalid")
	}
	writers := []string{filepath.Join(releaseRoot, "solovey-owner-manifest")}
	if executable, err := os.Readlink("/proc/self/exe"); err == nil && filepath.IsAbs(executable) {
		writers = append(writers, filepath.Join(filepath.Dir(executable), "solovey-owner-manifest"))
	}
	for _, writer := range writers {
		if !safeReleaseExecutable(writer) {
			continue
		}
		command := exec.CommandContext(ctx, writer)
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		return command.Run()
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
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.StatePath), 0o700); err != nil {
		return err
	}
	temporary := h.StatePath + ".incoming"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, h.StatePath); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return syncDirectory(filepath.Dir(h.StatePath))
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
		digest, err := fileDigest(path)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode().Perm()&0o022 != 0 || !info.Mode().IsRegular() {
			return errors.New("release client executable is unsafe")
		}
		entry.Executable, entry.ExecutableDigest, entry.Device, entry.Inode = path, digest, uint64(stat.Dev), stat.Ino
	}
	manifest, err = broker.FinalizeManifest(manifest)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(manifest)
	temporary := h.Manifest + ".incoming"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o640); err != nil {
		return err
	}
	if err := os.Chown(temporary, 0, 0); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, h.Manifest); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return syncDirectory(filepath.Dir(h.Manifest))
}

func extractRelease(archive, destination string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	count, total := 0, int64(0)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(header.Name)
		prefix := "solovey-ui/"
		if !strings.HasPrefix(name, prefix) {
			return errors.New("release archive root is invalid")
		}
		relative := strings.TrimPrefix(name, prefix)
		if relative == "" {
			continue
		}
		if !allowedReleasePath(relative) || header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > 512<<20 {
			return errors.New("release archive member is invalid")
		}
		count++
		total += header.Size
		if count > 64 || total > 1<<30 {
			return errors.New("release archive exceeds bounds")
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		if !pathWithin(destination, target) {
			return errors.New("release archive path escaped")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if executableReleasePath(relative) {
			mode = 0o755
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		written, copyErr := io.Copy(output, io.LimitReader(reader, header.Size+1))
		syncErr, closeErr := output.Sync(), output.Close()
		if copyErr != nil || syncErr != nil || closeErr != nil || written != header.Size {
			return errors.Join(copyErr, syncErr, closeErr, errors.New("release member size mismatch"))
		}
	}
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
	return ok && stat.Uid == 0 && stat.Gid == 0
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

func validateDiskState(state diskState) error {
	validRelease := func(value string) bool {
		return value == "" || len(value) <= 128 && filepath.Base(value) == value && value != "." && value != "current" && !strings.HasSuffix(value, ".staging")
	}
	validSequence := func(sequence uint64, digest string) bool {
		return sequence == 0 && digest == "" || sequence > 0 && validDigest(digest)
	}
	if !validRelease(state.ActiveRelease) || !validRelease(state.RollbackRelease) ||
		!validSequence(state.ActiveSequence, state.ActiveDigest) || !validSequence(state.VerifiedSequence, state.VerifiedDigest) ||
		!validSequence(state.RollbackSequence, state.RollbackDigest) ||
		state.VerifiedSequence > 0 && (state.VerifiedSequence != state.ActiveSequence || state.VerifiedDigest != state.ActiveDigest) ||
		state.ActiveRelease == "" && state.ActiveSequence > 0 || state.RollbackRelease == "" && state.RollbackSequence > 0 {
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
