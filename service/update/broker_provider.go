package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	backupdb "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	contract "github.com/MalenkiySolovey/solovey-ui/internal/ops/updatebroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
)

type BrokerProvider struct {
	Client  *broker.Client
	Fetcher release.Fetcher
	Source  release.Source
	Sources map[release.Channel]release.Source
	Root    string
}

func NewBrokerProvider(client *broker.Client, fetcher release.Fetcher, source release.Source) *BrokerProvider {
	if client == nil {
		client = broker.NewClient(broker.RolePanel)
	}
	return &BrokerProvider{Client: client, Fetcher: fetcher, Source: source,
		Sources: map[release.Channel]release.Source{release.ChannelMain: source, release.ChannelBeta: source},
		Root:    filepath.Join(configstorage.GetDBFolderPath(), "update-cache")}
}

func (p *BrokerProvider) Capabilities(ctx context.Context) Capabilities {
	result := Capabilities{Mode: "native", Check: "AVAILABLE", Download: "UNAVAILABLE", Prepare: "UNAVAILABLE",
		Activate: "UNAVAILABLE", Rollback: "UNAVAILABLE", OSUpdates: "EXTERNAL_MANAGED", Reboot: "OPERATOR_ADVISORY",
		ReasonCodes: []string{"privileged_update_broker_unavailable"}}
	if p != nil && p.Client != nil && runtime.GOOS == "linux" {
		var observed broker.CapabilitiesV1
		_, err := p.Client.Invoke(ctx, broker.Call{Verb: broker.VerbCapabilities, OperationID: "update-capabilities",
			Timeout: 3 * time.Second, Payload: contract.EmptyV1{}}, &observed)
		if err == nil && containsUpdateVerbs(observed.Verbs) && len(observed.Unresolved) == 0 {
			result.Download, result.Prepare, result.Activate, result.Rollback = "AVAILABLE", "AVAILABLE", "AVAILABLE", "AVAILABLE"
			result.ReasonCodes = []string{}
		}
	}
	result.Revision = semanticDigest(result)
	return result
}

func (p *BrokerProvider) DownloadAndStage(ctx context.Context, operation model.UpdateOperation, verified release.Verified,
	artifacts []release.Artifact, progress func(int64)) error {
	if p == nil || p.Client == nil || runtime.GOOS != "linux" || len(artifacts) == 0 {
		return ErrProviderUnavailable
	}
	total := int64(0)
	for _, artifact := range artifacts {
		if artifact.Size <= 0 || total > release.MaxReleaseSetBytes-artifact.Size {
			return errors.New("release artifact total exceeds limit")
		}
		total += artifact.Size
	}
	operationRoot := filepath.Join(p.Root, operation.OperationID)
	if err := os.MkdirAll(operationRoot, 0o700); err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if err := p.downloadAndStageArtifact(ctx, operation, verified, artifact, operationRoot, progress); err != nil {
			return err
		}
	}
	return nil
}

func (p *BrokerProvider) downloadAndStageArtifact(ctx context.Context, operation model.UpdateOperation, verified release.Verified,
	artifact release.Artifact, root string, progress func(int64)) error {
	cacheName := artifact.Role + "-" + artifact.Name
	partial := filepath.Join(root, cacheName+".partial")
	complete := filepath.Join(root, cacheName)
	_ = os.Remove(partial)
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	source, ok := p.Sources[release.Channel(operation.Channel)]
	if !ok {
		_ = file.Close()
		_ = os.Remove(partial)
		return errors.New("release channel source is unavailable")
	}
	written, fetchErr := p.Fetcher.FetchArtifact(ctx, source, artifact, file)
	syncErr := file.Sync()
	closeErr := file.Close()
	if fetchErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(partial)
		return errors.Join(fetchErr, syncErr, closeErr)
	}
	if err := os.Rename(partial, complete); err != nil {
		_ = os.Remove(partial)
		return err
	}
	if progress != nil {
		progress(written)
	}
	input, err := os.Open(complete)
	if err != nil {
		return err
	}
	defer input.Close()
	identity := releaseIdentity(operation, verified, []release.Artifact{artifact})
	buffer := make([]byte, contract.MaxChunkBytes)
	offset := int64(0)
	for {
		count, readErr := input.Read(buffer)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		if count == 0 {
			break
		}
		final := offset+int64(count) == artifact.Size
		request := contract.StageChunkRequestV1{Release: identity,
			Artifact: artifactIdentity(artifact),
			Offset:   offset, Chunk: append([]byte(nil), buffer[:count]...), Final: final}
		var result contract.StageChunkResultV1
		if err := p.mutate(ctx, broker.VerbUpdateStage, operation, uint64(offset/int64(contract.MaxChunkBytes))+1, request, &result); err != nil {
			return err
		}
		offset += int64(count)
		if result.ProviderRevision != contract.ProviderRevision || result.AcceptedBytes != offset || result.Complete != final ||
			(final && result.ArtifactDigest != artifact.SHA256) {
			return ErrRevisionMismatch
		}
	}
	return nil
}

func (p *BrokerProvider) Preflight(ctx context.Context, operation model.UpdateOperation, verified release.Verified,
	artifacts []release.Artifact) (PreflightResult, error) {
	if p == nil || p.Client == nil || runtime.GOOS != "linux" {
		return PreflightResult{}, ErrProviderUnavailable
	}
	backupRef, err := p.prepareDatabaseBackup(ctx, operation)
	if err != nil {
		return PreflightResult{}, err
	}
	request := contract.PrepareRequestV1{Release: releaseIdentity(operation, verified, artifacts),
		ExpectedBrokerCapability: operation.BrokerCapability, ExpectedManagementRevision: operation.DeploymentRevision}
	var result contract.PrepareResultV1
	if err := p.mutate(ctx, broker.VerbUpdatePrepare, operation, 800_001, request, &result); err != nil {
		return PreflightResult{}, err
	}
	return PreflightResult{RollbackAvailable: result.ProviderRevision == contract.ProviderRevision && result.ManagementReady &&
		validDigest(result.PreparedRef) && validDigest(result.RollbackRef), BackupRef: backupRef}, nil
}

func (p *BrokerProvider) CleanupTerminal(_ context.Context, operation model.UpdateOperation) error {
	if p == nil || filepath.Base(filepath.Clean(p.Root)) != "update-cache" || !safeID(operation.OperationID, 96) {
		return errors.New("update cache root is invalid")
	}
	root, err := filepath.Abs(p.Root)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	keep := ""
	if State(operation.State) == StateApplied {
		keep = operation.OperationID
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == keep || !strings.HasPrefix(name, "update-operation:") || !safeID(name, 96) {
			continue
		}
		path := filepath.Join(root, name)
		relative, relErr := filepath.Rel(root, path)
		info, infoErr := os.Lstat(path)
		if relErr != nil || relative != name || infoErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

func (p *BrokerProvider) prepareDatabaseBackup(ctx context.Context, operation model.UpdateOperation) (string, error) {
	if p == nil || !safeID(operation.OperationID, 96) {
		return "", ErrProviderUnavailable
	}
	path, cleanup, err := backupdb.PrepareExportContext(ctx, "")
	if err != nil {
		return "", err
	}
	defer cleanup()
	input, err := os.Open(path) // #nosec G304 -- generated bounded backup path.
	if err != nil {
		return "", err
	}
	rehearsal, err := backupdb.Rehearse(ctx, input)
	if closeErr := input.Close(); err == nil {
		err = closeErr
	}
	if err != nil || !rehearsal.Possible || !validDigest(rehearsal.BackupDigest) || rehearsal.BackupBytes <= 0 || rehearsal.BackupBytes > backupdb.MaxRestoreBytes {
		if err == nil {
			err = errors.New("update database rollback rehearsal failed")
		}
		return "", err
	}
	operationRoot := filepath.Join(p.Root, operation.OperationID)
	if err := os.MkdirAll(operationRoot, 0o700); err != nil {
		return "", err
	}
	destination := filepath.Join(operationRoot, "database-rollback.db")
	if digest, digestErr := boundedFileDigest(destination, backupdb.MaxRestoreBytes); digestErr == nil {
		if digest == rehearsal.BackupDigest {
			return digest, nil
		}
		return "", errors.New("update database rollback artifact changed")
	} else if !errors.Is(digestErr, os.ErrNotExist) {
		return "", digestErr
	}
	partial := destination + ".partial"
	_ = os.Remove(partial)
	output, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	input, err = os.Open(path) // #nosec G304 -- generated bounded backup path.
	if err != nil {
		_ = output.Close()
		_ = os.Remove(partial)
		return "", err
	}
	written, copyErr := io.Copy(output, io.LimitReader(&updateContextReader{ctx: ctx, reader: input}, backupdb.MaxRestoreBytes+1))
	syncErr, outputCloseErr, inputCloseErr := output.Sync(), output.Close(), input.Close()
	if copyErr != nil || syncErr != nil || outputCloseErr != nil || inputCloseErr != nil || written != rehearsal.BackupBytes || written > backupdb.MaxRestoreBytes {
		_ = os.Remove(partial)
		return "", errors.Join(copyErr, syncErr, outputCloseErr, inputCloseErr, errors.New("update database rollback copy failed"))
	}
	digest, err := boundedFileDigest(partial, backupdb.MaxRestoreBytes)
	if err != nil || digest != rehearsal.BackupDigest {
		_ = os.Remove(partial)
		return "", errors.Join(err, errors.New("update database rollback digest changed"))
	}
	if err := os.Rename(partial, destination); err != nil {
		_ = os.Remove(partial)
		return "", err
	}
	return digest, nil
}

type updateContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *updateContextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func boundedFileDigest(path string, limit int64) (string, error) {
	file, err := os.Open(path) // #nosec G304 -- caller supplies an internal generated cache path.
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, limit+1))
	if err != nil || written <= 0 || written > limit {
		return "", errors.Join(err, errors.New("bounded file digest rejected input"))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (p *BrokerProvider) Activate(ctx context.Context, operation model.UpdateOperation) error {
	identity := releaseIdentityFromOperation(operation)
	ref := updateRef(operation, "prepared")
	rollback := updateRef(operation, "rollback")
	var result contract.ActivateResultV1
	if err := p.mutate(ctx, broker.VerbUpdateActivate, operation, 800_002, contract.ActivateRequestV1{Release: identity,
		PreparedRef: ref, RollbackRef: rollback, ExpectedMode: "native"}, &result); err != nil {
		return err
	}
	if result.ProviderRevision != contract.ProviderRevision || result.ActiveSequence != operation.Sequence || result.ActiveDigest != operation.ManifestDigest {
		return ErrRevisionMismatch
	}
	return nil
}

func (p *BrokerProvider) VerifyActive(ctx context.Context, operation model.UpdateOperation) (bool, error) {
	var result contract.VerifyResultV1
	err := p.mutate(ctx, broker.VerbUpdateVerify, operation, 800_003, contract.VerifyRequestV1{Release: releaseIdentityFromOperation(operation),
		PreparedRef: updateRef(operation, "prepared"), RollbackRef: updateRef(operation, "rollback")}, &result)
	if err != nil {
		if errors.Is(err, ErrProviderUnavailable) {
			return false, ErrRestartPending
		}
		return false, err
	}
	if !result.Verified && result.HealthRevision == "restart-pending" {
		return false, ErrRestartPending
	}
	return result.ProviderRevision == contract.ProviderRevision && result.Verified && result.ManagementReady &&
		result.VerifiedSequence == operation.Sequence && result.VerifiedDigest == operation.ManifestDigest, nil
}

func (p *BrokerProvider) Rollback(ctx context.Context, operation model.UpdateOperation) (bool, error) {
	var result contract.RollbackResultV1
	err := p.mutate(ctx, broker.VerbUpdateRollback, operation, 800_004, contract.RollbackRequestV1{Release: releaseIdentityFromOperation(operation),
		RollbackRef: updateRef(operation, "rollback"), ReasonCode: operation.ReasonCode}, &result)
	if err != nil {
		return false, err
	}
	return result.ProviderRevision == contract.ProviderRevision && result.RolledBack && result.ManagementReady, nil
}

func (p *BrokerProvider) Reconcile(ctx context.Context, operation model.UpdateOperation) (State, error) {
	if p == nil || p.Client == nil {
		return StateRecoveryRequired, ErrProviderUnavailable
	}
	var result contract.ObservationV1
	_, err := p.Client.Invoke(ctx, broker.Call{Verb: broker.VerbUpdateObserve, OperationID: operation.OperationID + "-reconcile",
		Timeout: 15 * time.Second, Payload: contract.EmptyV1{}}, &result)
	if err != nil || result.ProviderRevision != contract.ProviderRevision {
		return StateRecoveryRequired, ErrProviderUnavailable
	}
	switch {
	case result.VerifiedSequence == operation.Sequence && result.VerifiedDigest == operation.ManifestDigest && result.ManagementReady:
		return StateApplied, nil
	case result.ActiveSequence == operation.Sequence && result.ActiveDigest == operation.ManifestDigest:
		return StateVerifyingActive, nil
	case result.RollbackAvailable:
		return StateRollbackPending, nil
	default:
		return StateRecoveryRequired, ErrRecoveryRequired
	}
}

func (p *BrokerProvider) mutate(ctx context.Context, verb broker.Verb, operation model.UpdateOperation, ordinal uint64, payload, target any) error {
	if p == nil || p.Client == nil || !safeID(operation.OperationID, 96) || operation.Revision == 0 || !validDigest(operation.ManifestDigest) {
		return ErrProviderUnavailable
	}
	data, _ := json.Marshal(struct {
		Verb broker.Verb
		ID   string
		Rev  uint64
		Body any
	}{verb, operation.OperationID, operation.Revision, payload})
	digest := broker.Digest(data)
	_, err := p.Client.Invoke(ctx, broker.Call{Verb: verb, OperationID: operation.OperationID,
		IdempotencyKey: "update-" + digest[:48], Fence: broker.Fence{Resource: "release-activation", Sequence: operation.Revision*1_000_000 + ordinal,
			Token: broker.Digest([]byte(operation.OperationID + ":" + operation.ManifestDigest))},
		Expected: broker.Revisions{Provider: contract.ProviderRevision, Configuration: operation.DeploymentRevision},
		Timeout:  2 * time.Minute, Payload: payload}, target)
	if err != nil {
		var public *broker.PublicError
		if errors.As(err, &public) && (public.Code == broker.CodeFence || public.Code == broker.CodeRevision || public.Code == broker.CodeIdempotency) {
			return ErrRevisionMismatch
		}
		return ErrProviderUnavailable
	}
	return nil
}

func releaseIdentity(operation model.UpdateOperation, verified release.Verified, artifacts []release.Artifact) contract.ReleaseIdentityV1 {
	identity := releaseIdentityFromOperation(operation)
	identity.Artifacts = make([]contract.ArtifactIdentityV1, 0, len(artifacts))
	for _, artifact := range artifacts {
		identity.Artifacts = append(identity.Artifacts, artifactIdentity(artifact))
	}
	if verified.Digest != "" {
		identity.ManifestDigest = verified.Digest
	}
	return identity
}

func artifactIdentity(artifact release.Artifact) contract.ArtifactIdentityV1 {
	return contract.ArtifactIdentityV1{Name: artifact.Name, Role: artifact.Role, Platform: artifact.Platform, Arch: artifact.Arch,
		MediaType: artifact.MediaType, Size: artifact.Size, SHA256: artifact.SHA256, Provenance: artifact.Provenance}
}

func releaseIdentityFromOperation(operation model.UpdateOperation) contract.ReleaseIdentityV1 {
	return contract.ReleaseIdentityV1{ReleaseID: operation.ReleaseID, Sequence: operation.Sequence, Version: operation.Version, ManifestDigest: operation.ManifestDigest,
		ArtifactSetDigest: operation.ArtifactSetDigest, BinaryProfile: operation.BinaryProfile, DeploymentRevision: operation.DeploymentRevision,
		MigrationSetDigest: operation.MigrationSetDigest, RestartClass: operation.RestartClass, RollbackClass: operation.RollbackClass}
}

func updateRef(operation model.UpdateOperation, kind string) string {
	return broker.Digest([]byte(kind + ":" + operation.OperationID + ":" + operation.ManifestDigest))
}

func containsUpdateVerbs(verbs []broker.Verb) bool {
	set := map[broker.Verb]bool{}
	for _, verb := range verbs {
		set[verb] = true
	}
	for _, verb := range []broker.Verb{broker.VerbUpdateObserve, broker.VerbUpdateStage, broker.VerbUpdatePrepare,
		broker.VerbUpdateActivate, broker.VerbUpdateVerify, broker.VerbUpdateRollback} {
		if !set[verb] {
			return false
		}
	}
	return true
}

func IsDockerRuntime() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SUI_DEPLOYMENT_KIND")), "docker") {
		return true
	}
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func DockerCapabilities() Capabilities {
	result := Capabilities{Mode: "docker-operator-managed", Check: "AVAILABLE", Download: "UNAVAILABLE", Prepare: "UNAVAILABLE",
		Activate: "OPERATOR_MANAGED", Rollback: "OPERATOR_MANAGED", OSUpdates: "EXTERNAL_MANAGED", Reboot: "OPERATOR_ADVISORY",
		ReasonCodes: []string{"docker_runtime_operator_managed", "docker_socket_not_used"}}
	result.Revision = semanticDigest(result)
	return result
}

func (p *BrokerProvider) String() string { return fmt.Sprintf("update-broker(%s)", p.Source.ID) }
