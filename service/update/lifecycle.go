package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	configupdate "github.com/MalenkiySolovey/solovey-ui/config/update"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	componentprofile "github.com/MalenkiySolovey/solovey-ui/internal/components/profile"
	operationcoordination "github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type State string

const (
	StateUnknown          State = "UNKNOWN"
	StateUpdateAvailable  State = "UPDATE_AVAILABLE"
	StateDownloading      State = "DOWNLOADING"
	StateDownloaded       State = "DOWNLOADED"
	StateVerifying        State = "VERIFYING"
	StateVerified         State = "VERIFIED"
	StatePreflighting     State = "PREFLIGHTING"
	StatePrepared         State = "PREPARED"
	StateActivating       State = "ACTIVATING"
	StateVerifyingActive  State = "VERIFYING_ACTIVE"
	StateApplied          State = "APPLIED"
	StateRollbackPending  State = "ROLLBACK_PENDING"
	StateRollingBack      State = "ROLLING_BACK"
	StateRolledBack       State = "ROLLED_BACK"
	StateFailed           State = "FAILED"
	StateRecoveryRequired State = "RECOVERY_REQUIRED"
)

var (
	ErrSigningUnavailable  = errors.New("release signing trust is unavailable")
	ErrNotNewer            = errors.New("installed version is up to date")
	ErrProviderUnavailable = errors.New("native update provider is unavailable")
	ErrRevisionMismatch    = errors.New("update operation revision mismatch")
	ErrOperationConflict   = errors.New("another update operation is active")
	ErrReleaseChanged      = errors.New("verified release identity changed")
	ErrRecoveryRequired    = errors.New("update operation requires recovery")
	ErrRestartPending      = errors.New("update activation restart is pending")
	ErrResourcePressure    = errors.New("resource pressure blocks update preparation")
)

const activationHealthDeadline = 5 * time.Minute

type Capabilities struct {
	Mode        string   `json:"mode"`
	Check       string   `json:"check"`
	Download    string   `json:"download"`
	Prepare     string   `json:"prepare"`
	Activate    string   `json:"activate"`
	Rollback    string   `json:"rollback"`
	OSUpdates   string   `json:"osUpdates"`
	Reboot      string   `json:"reboot"`
	ReasonCodes []string `json:"reasonCodes"`
	Revision    string   `json:"revision"`
}

type CheckResult struct {
	State             State           `json:"state"`
	CurrentVersion    string          `json:"currentVersion"`
	ReleaseID         string          `json:"releaseId,omitempty"`
	Version           string          `json:"version,omitempty"`
	Channel           release.Channel `json:"channel"`
	Sequence          uint64          `json:"sequence,omitempty"`
	ManifestDigest    string          `json:"manifestDigest,omitempty"`
	SigningKeyID      string          `json:"signingKeyId,omitempty"`
	SigningStatus     string          `json:"signingStatus"`
	UpdateAvailable   bool            `json:"updateAvailable"`
	ArtifactSetDigest string          `json:"artifactSetDigest,omitempty"`
	RestartClass      string          `json:"restartClass,omitempty"`
	RebootClass       string          `json:"rebootClass,omitempty"`
	RollbackClass     string          `json:"rollbackClass,omitempty"`
	ReasonCodes       []string        `json:"reasonCodes"`
	Capabilities      Capabilities    `json:"capabilities"`
}

type LifecycleStatus struct {
	Schema        string                    `json:"schema"`
	State         State                     `json:"state"`
	SigningStatus string                    `json:"signingStatus"`
	Desired       UpdateDesiredState        `json:"desired"`
	Selected      *UpdateSelectedState      `json:"selected,omitempty"`
	Actual        UpdateActualState         `json:"actual"`
	Release       *model.UpdateReleaseState `json:"release,omitempty"`
	Operation     *model.UpdateOperation    `json:"operation,omitempty"`
	Capabilities  Capabilities              `json:"capabilities"`
	ReasonCodes   []string                  `json:"reasonCodes"`
	ObservedAt    int64                     `json:"observedAt"`
	FreshUntil    int64                     `json:"freshUntil,omitempty"`
}

type UpdateDesiredState struct {
	Channel release.Channel `json:"channel"`
}

type UpdateSelectedState struct {
	Channel        string `json:"channel"`
	ReleaseID      string `json:"releaseId"`
	Version        string `json:"version"`
	Sequence       uint64 `json:"sequence"`
	ManifestDigest string `json:"manifestDigest"`
	SigningKeyID   string `json:"signingKeyId"`
	VerifiedAt     int64  `json:"verifiedAt"`
}

type UpdateActualState struct {
	Version       string `json:"version"`
	BinaryProfile string `json:"binaryProfile"`
	Mode          string `json:"mode"`
}

type PrepareRequest struct {
	Channel                release.Channel
	ExpectedSequence       uint64
	ExpectedManifestDigest string
	IdempotencyKey         string
	Acknowledged           bool
}

type RevisionRequest struct {
	OperationID      string
	ExpectedRevision uint64
}

type HealthResult struct {
	Ready       bool     `json:"ready"`
	ReasonCodes []string `json:"reasonCodes"`
	Revision    string   `json:"revision"`
}

type Provider interface {
	Capabilities(context.Context) Capabilities
	DownloadAndStage(context.Context, model.UpdateOperation, release.Verified, []release.Artifact, func(int64)) error
	Preflight(context.Context, model.UpdateOperation, release.Verified, []release.Artifact) (PreflightResult, error)
	Activate(context.Context, model.UpdateOperation) error
	VerifyActive(context.Context, model.UpdateOperation) (bool, error)
	Rollback(context.Context, model.UpdateOperation) (bool, error)
	Reconcile(context.Context, model.UpdateOperation) (State, error)
}

type terminalCleaner interface {
	CleanupTerminal(context.Context, model.UpdateOperation) error
}

type PreflightResult struct {
	RollbackAvailable bool
	BackupRef         string
}

type UnavailableProvider struct{ Mode string }

func (p UnavailableProvider) Capabilities(context.Context) Capabilities {
	mode := p.Mode
	if mode == "" {
		mode = "native"
	}
	if mode == "docker-operator-managed" {
		return DockerCapabilities()
	}
	result := Capabilities{Mode: mode, Check: "AVAILABLE", Download: "UNAVAILABLE", Prepare: "UNAVAILABLE",
		Activate: "UNAVAILABLE", Rollback: "UNAVAILABLE", OSUpdates: "EXTERNAL_MANAGED", Reboot: "OPERATOR_ADVISORY",
		ReasonCodes: []string{"privileged_update_broker_unavailable"}}
	result.Revision = semanticDigest(result)
	return result
}
func (UnavailableProvider) DownloadAndStage(context.Context, model.UpdateOperation, release.Verified, []release.Artifact, func(int64)) error {
	return ErrProviderUnavailable
}
func (UnavailableProvider) Preflight(context.Context, model.UpdateOperation, release.Verified, []release.Artifact) (PreflightResult, error) {
	return PreflightResult{}, ErrProviderUnavailable
}
func (UnavailableProvider) Activate(context.Context, model.UpdateOperation) error {
	return ErrProviderUnavailable
}
func (UnavailableProvider) VerifyActive(context.Context, model.UpdateOperation) (bool, error) {
	return false, ErrProviderUnavailable
}
func (UnavailableProvider) Rollback(context.Context, model.UpdateOperation) (bool, error) {
	return false, ErrProviderUnavailable
}
func (UnavailableProvider) Reconcile(context.Context, model.UpdateOperation) (State, error) {
	return StateRecoveryRequired, ErrProviderUnavailable
}

type ReleaseClient interface {
	Fetch(context.Context, release.Source) ([]byte, error)
}

type Repository struct{ DB func() *gorm.DB }

type LifecycleManager struct {
	mu       sync.Mutex
	repo     Repository
	provider Provider
	fetcher  ReleaseClient
	sources  map[release.Channel]release.Source
	trust    release.TrustStore
	now      func() time.Time
	platform func() (string, string)
	profile  func() string
	health   func(context.Context, model.UpdateOperation) HealthResult
	admit    func(string) bool
}

var lifecycleShared = newSharedLifecycle()

func SharedLifecycle() *LifecycleManager { return lifecycleShared }

func newSharedLifecycle() *LifecycleManager {
	trust, _ := configupdate.ReleaseTrustStore()
	sources := map[release.Channel]release.Source{
		release.ChannelMain: {ID: "solovey-production-main", Origin: "https://github.com",
			ManifestPath:       "/MalenkiySolovey/solovey-ui/releases/download/channel-main/solovey-ui-release.json",
			ExpectedProvenance: "github-actions",
			RedirectOrigins:    []string{"https://release-assets.githubusercontent.com"}},
		release.ChannelBeta: {ID: "solovey-production-beta", Origin: "https://github.com",
			ManifestPath:       "/MalenkiySolovey/solovey-ui/releases/download/channel-beta/solovey-ui-release.json",
			ExpectedProvenance: "github-actions",
			RedirectOrigins:    []string{"https://release-assets.githubusercontent.com"}},
	}
	fetcher := release.Fetcher{}
	var provider Provider = UnavailableProvider{Mode: "unsupported-platform"}
	if runtime.GOOS == "linux" {
		if IsDockerRuntime() {
			provider = UnavailableProvider{Mode: "docker-operator-managed"}
		} else {
			brokerProvider := NewBrokerProvider(nil, fetcher, sources[release.ChannelMain])
			brokerProvider.Sources = sources
			provider = brokerProvider
		}
	}
	manager := NewLifecycleManager(Repository{DB: dbsqlite.DB}, provider, fetcher, sources[release.ChannelMain], trust)
	manager.sources = sources
	return manager
}

func NewLifecycleManager(repo Repository, provider Provider, fetcher ReleaseClient, source release.Source, trust release.TrustStore) *LifecycleManager {
	if provider == nil {
		provider = UnavailableProvider{}
	}
	if fetcher == nil {
		fetcher = release.Fetcher{}
	}
	return &LifecycleManager{repo: repo, provider: provider, fetcher: fetcher,
		sources: map[release.Channel]release.Source{release.ChannelMain: source, release.ChannelBeta: source}, trust: trust,
		now: time.Now, platform: func() (string, string) {
			arch := configupdate.ResolveArtifactPlatform()
			if arch == "" {
				arch = runtime.GOARCH
			}
			return runtime.GOOS, arch
		}, profile: func() string { return componentprofile.Binary }}
}

func (m *LifecycleManager) SetHealthCheck(check func(context.Context, model.UpdateOperation) HealthResult) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.health = check
	m.mu.Unlock()
}

func (m *LifecycleManager) SetAdmissionCheck(check func(string) bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.admit = check
	m.mu.Unlock()
}

func (m *LifecycleManager) Capabilities(ctx context.Context) Capabilities {
	if m == nil || m.provider == nil {
		return UnavailableProvider{}.Capabilities(ctx)
	}
	return m.provider.Capabilities(ctx)
}

func (m *LifecycleManager) Status(ctx context.Context, channel release.Channel) LifecycleStatus {
	if m == nil {
		capabilities := UnavailableProvider{}.Capabilities(ctx)
		return LifecycleStatus{Schema: "solovey.update-posture/v1", State: StateUnknown, SigningStatus: "SIGNING_UNAVAILABLE",
			Desired: UpdateDesiredState{Channel: channel}, Actual: UpdateActualState{Version: configidentity.GetVersion(), Mode: capabilities.Mode},
			Capabilities: capabilities, ReasonCodes: []string{"update_manager_unavailable"}, ObservedAt: time.Now().Unix()}
	}
	now := m.now()
	capabilities := m.Capabilities(ctx)
	trustAvailable := m.trust.Available(now)
	result := LifecycleStatus{Schema: "solovey.update-posture/v1", State: StateUnknown, SigningStatus: "SIGNING_UNAVAILABLE",
		Desired:      UpdateDesiredState{Channel: channel},
		Actual:       UpdateActualState{Version: configidentity.GetVersion(), BinaryProfile: m.profile(), Mode: capabilities.Mode},
		Capabilities: capabilities, ReasonCodes: []string{}, ObservedAt: now.Unix()}
	if trustAvailable {
		result.SigningStatus = "AVAILABLE_NOT_YET_VERIFIED"
	} else {
		result.ReasonCodes = append(result.ReasonCodes, "production_release_trust_unavailable")
	}
	if row, err := m.repo.releaseState(ctx, channel); err == nil {
		switch {
		case !validPersistedReleaseState(row):
			result.State = StateRecoveryRequired
			result.ReasonCodes = append(result.ReasonCodes, "release_state_invalid")
		case restoredReleaseState(row):
			result.Release = &row
			result.ReasonCodes = append(result.ReasonCodes, "restored_release_state_untrusted")
		default:
			result.Release = &row
			result.Selected = &UpdateSelectedState{Channel: row.Channel, ReleaseID: row.ReleaseID, Version: row.Version, Sequence: row.LastVerifiedSequence,
				ManifestDigest: row.ManifestDigest, SigningKeyID: row.SigningKeyID, VerifiedAt: row.UpdatedAt}
			result.FreshUntil = row.ExpiresAt
			switch {
			case row.ExpiresAt <= now.Unix():
				result.SigningStatus = "VERIFIED_STALE"
				result.ReasonCodes = append(result.ReasonCodes, "verified_release_metadata_stale")
			case !trustAvailable:
				result.SigningStatus = "PREVIOUSLY_VERIFIED_TRUST_UNAVAILABLE"
			case versionpolicy.VersionIsNewer(row.Version, result.Actual.Version):
				result.SigningStatus = "VERIFIED"
				result.State = StateUpdateAvailable
				result.ReasonCodes = append(result.ReasonCodes, "newer_verified_release_available")
			default:
				result.SigningStatus = "VERIFIED"
				result.ReasonCodes = append(result.ReasonCodes, "installed_version_not_older")
			}
		}
	}
	if operation, err := m.repo.activeOrRecovery(ctx); err == nil {
		if !validPersistedOperation(operation) {
			operation = operationRecoveryProjection(operation)
		}
		result.Operation = &operation
		result.State = State(operation.State)
		if operation.RestoredUntrusted {
			result.State = StateRecoveryRequired
			result.ReasonCodes = append(result.ReasonCodes, "restored_update_state_untrusted")
		}
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		if latest, latestErr := m.repo.latest(ctx); latestErr == nil {
			if !validPersistedOperation(latest) {
				latest = operationRecoveryProjection(latest)
			}
			result.Operation = &latest
			result.State = State(latest.State)
		}
	}
	if len(result.ReasonCodes) == 0 {
		result.ReasonCodes = []string{"update_status_observed"}
	}
	return result
}

func (m *LifecycleManager) Check(ctx context.Context, channel release.Channel) (CheckResult, error) {
	result := CheckResult{State: StateUnknown, CurrentVersion: configidentity.GetVersion(), Channel: channel,
		SigningStatus: "SIGNING_UNAVAILABLE", ReasonCodes: []string{}, Capabilities: m.Capabilities(ctx)}
	if m == nil || !m.trust.Available(m.now()) {
		result.ReasonCodes = []string{"production_release_trust_unavailable"}
		return result, ErrSigningUnavailable
	}
	lastApplied, err := m.repo.lastApplied(ctx, channel)
	if err != nil {
		result.ReasonCodes = []string{"release_state_unavailable"}
		return result, err
	}
	verified, artifacts, err := m.fetchAndVerify(ctx, channel, lastApplied)
	if err != nil {
		result.SigningStatus = "VERIFICATION_FAILED"
		result.ReasonCodes = []string{"release_verification_failed"}
		return result, err
	}
	available := versionpolicy.VersionIsNewer(verified.Manifest.Version, result.CurrentVersion)
	result.SigningStatus = "VERIFIED"
	result.ReleaseID, result.Version, result.Sequence = verified.Manifest.ReleaseID, verified.Manifest.Version, verified.Manifest.Sequence
	result.ManifestDigest, result.SigningKeyID = verified.Digest, verified.KeyID
	result.UpdateAvailable, result.ArtifactSetDigest = available, release.ArtifactSetDigest(artifacts)
	result.RestartClass, result.RebootClass, result.RollbackClass = verified.Manifest.RestartClass, verified.Manifest.RebootClass, verified.Manifest.RollbackClass
	if available {
		result.State, result.ReasonCodes = StateUpdateAvailable, []string{"newer_verified_release_available"}
	} else {
		result.State, result.ReasonCodes = StateUnknown, []string{"installed_version_not_older"}
	}
	if err := m.repo.saveVerified(ctx, verified, channel, available, m.now().Unix()); err != nil {
		return result, err
	}
	return result, nil
}

func (m *LifecycleManager) Prepare(ctx context.Context, request PrepareRequest) (model.UpdateOperation, error) {
	if m == nil || !safeID(request.IdempotencyKey, 96) || request.ExpectedSequence == 0 ||
		!validDigest(request.ExpectedManifestDigest) || !request.Acknowledged {
		return model.UpdateOperation{}, errors.New("invalid update preparation request")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, err := m.repo.byIdempotency(ctx, request.IdempotencyKey); err == nil {
		if existing.Sequence == request.ExpectedSequence && existing.ManifestDigest == request.ExpectedManifestDigest {
			return existing, nil
		}
		return model.UpdateOperation{}, ErrReleaseChanged
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.UpdateOperation{}, err
	}
	if active, err := m.repo.active(ctx); err == nil && active.OperationID != "" {
		return active, ErrOperationConflict
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.UpdateOperation{}, err
	}
	if blocker, err := m.repo.globalBlocker(ctx); err != nil {
		return model.UpdateOperation{}, err
	} else if blocker != "" {
		return model.UpdateOperation{}, ErrOperationConflict
	}
	if m.admit != nil && !m.admit("heavy_mutation") {
		return model.UpdateOperation{}, ErrResourcePressure
	}
	lastApplied, err := m.repo.lastApplied(ctx, request.Channel)
	if err != nil {
		return model.UpdateOperation{}, err
	}
	verified, artifacts, err := m.fetchAndVerify(ctx, request.Channel, lastApplied)
	if err != nil || verified.Manifest.Sequence != request.ExpectedSequence || verified.Digest != request.ExpectedManifestDigest {
		return model.UpdateOperation{}, ErrReleaseChanged
	}
	if !versionpolicy.VersionIsNewer(verified.Manifest.Version, configidentity.GetVersion()) {
		return model.UpdateOperation{}, ErrNotNewer
	}
	platform, arch := m.platform()
	operation := newUpdateOperation(request, verified, artifacts, platform, arch, m.profile(), m.now())
	// Prepare is a valid entry point even when the caller did not run Check
	// first. Recheck durable sequence and blockers while cross-domain admission
	// is serialized, then persist verified identity followed by the operation
	// before any artifact can be staged.
	if err := operationcoordination.SerializeAdmission(func() error {
		if active, activeErr := m.repo.active(ctx); activeErr == nil && active.OperationID != "" {
			return ErrOperationConflict
		} else if activeErr != nil && !errors.Is(activeErr, gorm.ErrRecordNotFound) {
			return activeErr
		}
		if blocker, blockerErr := m.repo.globalBlocker(ctx); blockerErr != nil {
			return blockerErr
		} else if blocker != "" {
			return ErrOperationConflict
		}
		currentLastApplied, appliedErr := m.repo.lastApplied(ctx, request.Channel)
		if appliedErr != nil {
			return appliedErr
		}
		if currentLastApplied >= verified.Manifest.Sequence {
			return ErrReleaseChanged
		}
		if saveErr := m.repo.saveVerified(ctx, verified, request.Channel, true, m.now().Unix()); saveErr != nil {
			return saveErr
		}
		return m.repo.create(ctx, operation, "update_preparation_started", "")
	}); err != nil {
		return model.UpdateOperation{}, err
	}
	progress := func(bytes int64) { _ = m.repo.progress(context.Background(), operation.OperationID, bytes) }
	if err := m.provider.DownloadAndStage(ctx, operation, verified, artifacts, progress); err != nil {
		return m.fail(ctx, operation, "update_download_failed", err)
	}
	operation, err = m.advance(ctx, operation, StateDownloaded, "release_artifacts_downloaded", "")
	if err != nil {
		return operation, err
	}
	operation, err = m.advance(ctx, operation, StateVerifying, "release_artifacts_verifying", "")
	if err != nil {
		return operation, err
	}
	operation, err = m.advance(ctx, operation, StateVerified, "release_artifacts_verified", "")
	if err != nil {
		return operation, err
	}
	operation, err = m.advance(ctx, operation, StatePreflighting, "update_preflight_started", "")
	if err != nil {
		return operation, err
	}
	preflight, preflightErr := m.provider.Preflight(ctx, operation, verified, artifacts)
	if preflightErr != nil || !preflight.RollbackAvailable || !validDigest(preflight.BackupRef) {
		return m.fail(ctx, operation, "update_preflight_failed", preflightErr)
	}
	operation.RollbackAvailable, operation.BackupRef = true, preflight.BackupRef
	return m.advance(ctx, operation, StatePrepared, "update_prepared", "")
}

func (m *LifecycleManager) Activate(ctx context.Context, request RevisionRequest) (model.UpdateOperation, error) {
	if m == nil || !safeID(request.OperationID, 96) || request.ExpectedRevision == 0 {
		return model.UpdateOperation{}, errors.New("invalid update activation request")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	operation, err := m.repo.operation(ctx, request.OperationID)
	if err != nil {
		return model.UpdateOperation{}, err
	}
	if !validPersistedOperation(operation) {
		return operationRecoveryProjection(operation), ErrRecoveryRequired
	}
	if operation.Revision != request.ExpectedRevision || State(operation.State) != StatePrepared || operation.RestoredUntrusted {
		return operation, ErrRevisionMismatch
	}
	operation, err = m.advance(ctx, operation, StateActivating, "update_activation_started", "")
	if err != nil {
		return operation, err
	}
	if err := m.provider.Activate(ctx, operation); err != nil {
		return m.rollbackAfterFailure(ctx, operation, "update_activation_failed", err)
	}
	operation, err = m.advance(ctx, operation, StateVerifyingActive, "active_release_verifying", "")
	if err != nil {
		return operation, err
	}
	verified, verifyErr := m.provider.VerifyActive(ctx, operation)
	if errors.Is(verifyErr, ErrRestartPending) {
		return operation, nil
	}
	if verifyErr != nil || !verified {
		return m.rollbackAfterFailure(ctx, operation, "active_release_verification_failed", verifyErr)
	}
	if health := m.checkHealth(ctx, operation); !health.Ready {
		return m.rollbackAfterFailure(ctx, operation, "active_release_health_failed", ErrHealthCheckFailed{ReasonCodes: health.ReasonCodes})
	}
	applied, err := m.advance(ctx, operation, StateApplied, "update_applied", "")
	if err == nil {
		err = m.cleanupTerminal(ctx, applied)
	}
	return applied, err
}

func (m *LifecycleManager) Rollback(ctx context.Context, request RevisionRequest) (model.UpdateOperation, error) {
	if m == nil || !safeID(request.OperationID, 96) || request.ExpectedRevision == 0 {
		return model.UpdateOperation{}, errors.New("invalid update rollback request")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	operation, err := m.repo.operation(ctx, request.OperationID)
	if err != nil {
		return model.UpdateOperation{}, err
	}
	if !validPersistedOperation(operation) {
		return operationRecoveryProjection(operation), ErrRecoveryRequired
	}
	if operation.Revision != request.ExpectedRevision || !operation.RollbackAvailable || operation.RestoredUntrusted ||
		!rollbackAllowedFrom(State(operation.State)) {
		return operation, ErrRevisionMismatch
	}
	if active, activeErr := m.repo.active(ctx); activeErr == nil && active.OperationID != operation.OperationID {
		return operation, ErrOperationConflict
	} else if activeErr != nil && !errors.Is(activeErr, gorm.ErrRecordNotFound) {
		return operation, activeErr
	}
	if State(operation.State) == StateApplied {
		releaseState, stateErr := m.repo.releaseState(ctx, release.Channel(operation.Channel))
		if stateErr != nil {
			return operation, stateErr
		}
		if !validPersistedReleaseState(releaseState) || restoredReleaseState(releaseState) {
			return operationRecoveryProjection(operation), ErrRecoveryRequired
		}
		if releaseState.LastAppliedSequence != operation.Sequence {
			return operation, ErrRevisionMismatch
		}
	}
	result := operation
	err = operationcoordination.SerializeAdmission(func() error {
		if blocker, blockerErr := m.repo.globalBlocker(ctx); blockerErr != nil {
			return blockerErr
		} else if blocker != "" {
			return ErrOperationConflict
		}
		var rollbackErr error
		result, rollbackErr = m.performRollback(ctx, operation, "operator_rollback_requested")
		return rollbackErr
	})
	return result, err
}

func (m *LifecycleManager) Operation(ctx context.Context, id string) (model.UpdateOperation, error) {
	if !safeID(id, 96) {
		return model.UpdateOperation{}, errors.New("invalid update operation id")
	}
	operation, err := m.repo.operation(ctx, id)
	if err == nil && !validPersistedOperation(operation) {
		return operationRecoveryProjection(operation), nil
	}
	return operation, err
}

func (m *LifecycleManager) Timeline(ctx context.Context, id string, after uint64, limit int) ([]model.UpdateJournal, bool, error) {
	if m == nil || !safeID(id, 96) || !strings.HasPrefix(id, "update-operation:") || limit < 1 || limit > 200 {
		return nil, false, errors.New("invalid update timeline request")
	}
	return m.repo.timeline(ctx, id, after, limit)
}

func (m *LifecycleManager) ActiveOrRecovery(ctx context.Context) (model.UpdateOperation, error) {
	operation, err := m.repo.activeOrRecovery(ctx)
	if err == nil && !validPersistedOperation(operation) {
		return operationRecoveryProjection(operation), nil
	}
	return operation, err
}

func (m *LifecycleManager) ReconcileStartup(ctx context.Context) error {
	if m == nil {
		return ErrProviderUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	operation, err := m.repo.active(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !validPersistedOperation(operation) {
		return ErrRecoveryRequired
	}
	state, reconcileErr := m.provider.Reconcile(ctx, operation)
	if reconcileErr != nil || state == StateRecoveryRequired || state == StateUnknown {
		_, updateErr := m.advance(ctx, operation, StateRecoveryRequired, "startup_reconciliation_ambiguous", "update_reconciliation_ambiguous")
		if updateErr != nil {
			return updateErr
		}
		return ErrRecoveryRequired
	}
	if state == StateVerifyingActive {
		if operation.UpdatedAt > 0 && m.now().Sub(time.Unix(operation.UpdatedAt, 0)) >= activationHealthDeadline {
			_, rollbackErr := m.rollbackAfterFailure(ctx, operation, "active_release_health_timeout", errors.New("active release health deadline exceeded"))
			return rollbackErr
		}
		if State(operation.State) == StateVerifyingActive {
			return nil
		}
	}
	if state == StateRollbackPending {
		if State(operation.State) != StateRollbackPending {
			operation, err = m.advance(ctx, operation, StateRollbackPending, "startup_rollback_pending", "startup_reconciliation_requires_rollback")
			if err != nil {
				return err
			}
		}
		_, err = m.performRollback(ctx, operation, "startup_reconciliation_requires_rollback")
		return err
	}
	if state == StateApplied {
		if health := m.checkHealth(ctx, operation); !health.Ready {
			_, rollbackErr := m.rollbackAfterFailure(ctx, operation, "active_release_health_failed", ErrHealthCheckFailed{ReasonCodes: health.ReasonCodes})
			return rollbackErr
		}
	}
	operation, err = m.advance(ctx, operation, state, "startup_reconciled", "")
	if err == nil && state == StateApplied {
		err = m.cleanupTerminal(ctx, operation)
	}
	return err
}

type ErrHealthCheckFailed struct{ ReasonCodes []string }

func (e ErrHealthCheckFailed) Error() string {
	if len(e.ReasonCodes) == 0 {
		return "active release health check failed"
	}
	return "active release health check failed: " + strings.Join(e.ReasonCodes, ",")
}

func (m *LifecycleManager) checkHealth(ctx context.Context, operation model.UpdateOperation) HealthResult {
	if m.health == nil {
		return HealthResult{Ready: false, ReasonCodes: []string{"health_check_unavailable"}, Revision: semanticDigest("health-check-unavailable")}
	}
	result := m.health(ctx, operation)
	if len(result.ReasonCodes) > 16 {
		result.ReasonCodes = append([]string(nil), result.ReasonCodes[:16]...)
	}
	for index, reason := range result.ReasonCodes {
		if !safeID(reason, 96) {
			result.ReasonCodes[index] = "health_reason_invalid"
		}
	}
	if !validDigest(result.Revision) {
		result.Ready = false
		result.Revision = semanticDigest(struct {
			Ready   bool
			Reasons []string
		}{result.Ready, result.ReasonCodes})
		if len(result.ReasonCodes) == 0 {
			result.ReasonCodes = []string{"health_revision_invalid"}
		}
	}
	return result
}

func (m *LifecycleManager) fetchAndVerify(ctx context.Context, channel release.Channel, lastApplied uint64) (release.Verified, []release.Artifact, error) {
	source, ok := m.sources[channel]
	if !ok {
		return release.Verified{}, nil, errors.New("release channel source is unavailable")
	}
	raw, err := m.fetcher.Fetch(ctx, source)
	if err != nil {
		return release.Verified{}, nil, err
	}
	verified, err := release.Verify(raw, m.trust, m.now(), channel, lastApplied)
	if err != nil {
		return release.Verified{}, nil, err
	}
	if err := validateManifestCompatibility(verified.Manifest); err != nil {
		return release.Verified{}, nil, err
	}
	for _, artifact := range verified.Manifest.Artifacts {
		if artifact.Provenance != source.ExpectedProvenance {
			return release.Verified{}, nil, errors.New("release artifact provenance does not match the pinned source policy")
		}
	}
	platform, arch := m.platform()
	artifacts, err := verified.Manifest.ArtifactsFor(platform, arch, m.profile())
	return verified, artifacts, err
}

func validateManifestCompatibility(manifest release.Manifest) error {
	if manifest.BrokerCapability != broker.CapabilityRevision {
		return errors.New("release broker capability is incompatible")
	}
	currentVersion := configidentity.GetVersion()
	minimumPanelComparison, minimumPanelOK := versionpolicy.CompareVersions(currentVersion, manifest.MinimumPanelVersion)
	maximumPanelComparison, maximumPanelOK := versionpolicy.CompareVersions(currentVersion, manifest.MaximumPanelVersion)
	if !minimumPanelOK || !maximumPanelOK || minimumPanelComparison < 0 || maximumPanelComparison > 0 {
		return errors.New("release panel version range is incompatible")
	}
	minimumComparison, minimumOK := versionpolicy.CompareVersions(manifest.MinimumCoreSchema, "1.11")
	maximumComparison, maximumOK := versionpolicy.CompareVersions(manifest.MaximumCoreSchema, "1.11")
	targetComparison, targetOK := versionpolicy.CompareVersions(manifest.TargetCoreSchema, "1.11")
	if !minimumOK || !maximumOK || !targetOK || minimumComparison > 0 || maximumComparison < 0 || targetComparison != 0 {
		return errors.New("release core schema is incompatible")
	}
	components := make(map[string]release.Component, len(manifest.Components))
	for _, component := range manifest.Components {
		min, minOK := versionpolicy.CompareVersions(component.MinimumCoreSchema, manifest.TargetCoreSchema)
		max, maxOK := versionpolicy.CompareVersions(component.MaximumCoreSchema, manifest.TargetCoreSchema)
		if !minOK || !maxOK || min > 0 || max < 0 {
			return errors.New("release component schema range is incompatible")
		}
		components[component.ID] = component
	}
	installed, err := installstate.InstalledComponents()
	if err != nil {
		return errors.New("installed component inventory is unavailable")
	}
	for _, owner := range installed {
		if _, included := components[owner.ID]; !included {
			return fmt.Errorf("installed component %s is absent from the release", owner.ID)
		}
	}
	return nil
}

func (m *LifecycleManager) fail(ctx context.Context, operation model.UpdateOperation, reason string, cause error) (model.UpdateOperation, error) {
	failed, err := m.advance(ctx, operation, StateFailed, "update_failed", reason)
	if err != nil {
		return failed, err
	}
	if cause == nil {
		cause = errors.New(reason)
	}
	return failed, errors.Join(cause, m.cleanupTerminal(ctx, failed))
}

func (m *LifecycleManager) rollbackAfterFailure(ctx context.Context, operation model.UpdateOperation, reason string, cause error) (model.UpdateOperation, error) {
	operation, err := m.advance(ctx, operation, StateRollbackPending, "update_rollback_pending", reason)
	if err != nil {
		return operation, err
	}
	rolledBack, rollbackErr := m.performRollback(ctx, operation, reason)
	if rollbackErr != nil {
		return rolledBack, rollbackErr
	}
	if cause == nil {
		cause = errors.New(reason)
	}
	return rolledBack, cause
}

func (m *LifecycleManager) performRollback(ctx context.Context, operation model.UpdateOperation, reason string) (model.UpdateOperation, error) {
	if !rollbackAllowedFrom(State(operation.State)) {
		return operation, ErrRevisionMismatch
	}
	operation, err := m.advance(ctx, operation, StateRollingBack, "update_rollback_started", reason)
	if err != nil {
		return operation, err
	}
	verified, rollbackErr := m.provider.Rollback(ctx, operation)
	if rollbackErr != nil || !verified {
		recovery, updateErr := m.advance(ctx, operation, StateRecoveryRequired, "update_rollback_ambiguous", "update_rollback_ambiguous")
		if updateErr != nil {
			return recovery, updateErr
		}
		return recovery, ErrRecoveryRequired
	}
	operation.RollbackAvailable = false
	rolledBack, err := m.advance(ctx, operation, StateRolledBack, "update_rolled_back", reason)
	if err == nil {
		err = m.cleanupTerminal(ctx, rolledBack)
	}
	return rolledBack, err
}

func rollbackAllowedFrom(state State) bool {
	switch state {
	case StatePrepared, StateActivating, StateVerifyingActive, StateApplied, StateRollbackPending:
		return true
	default:
		return false
	}
}

func (m *LifecycleManager) cleanupTerminal(ctx context.Context, operation model.UpdateOperation) error {
	if cleaner, ok := m.provider.(terminalCleaner); ok {
		return cleaner.CleanupTerminal(ctx, operation)
	}
	return nil
}

func (m *LifecycleManager) advance(ctx context.Context, operation model.UpdateOperation, state State, event, reason string) (model.UpdateOperation, error) {
	previous := operation
	previousState, previousRevision := operation.State, operation.Revision
	operation.State, operation.ReasonCode, operation.Revision, operation.UpdatedAt = string(state), reason, operation.Revision+1, m.now().Unix()
	var err error
	if state == StateApplied {
		err = m.repo.completeApplied(ctx, operation, previousRevision, previousState, event)
	} else {
		err = m.repo.update(ctx, operation, previousRevision, previousState, event)
	}
	if err != nil {
		return previous, err
	}
	return operation, nil
}

func newUpdateOperation(request PrepareRequest, verified release.Verified, artifacts []release.Artifact, platform, arch, profile string, now time.Time) model.UpdateOperation {
	identity := semanticDigest(struct{ Idempotency, Manifest string }{request.IdempotencyKey, verified.Digest})
	total := int64(0)
	for _, artifact := range artifacts {
		total += artifact.Size
	}
	return model.UpdateOperation{OperationID: "update-operation:" + identity[:48], IdempotencyKey: request.IdempotencyKey,
		State: string(StateDownloading), Channel: string(request.Channel), Sequence: verified.Manifest.Sequence,
		ReleaseID: verified.Manifest.ReleaseID, Version: verified.Manifest.Version,
		ManifestDigest: verified.Digest, ArtifactSetDigest: release.ArtifactSetDigest(artifacts), Platform: platform, Arch: arch,
		BinaryProfile:      profile,
		DeploymentRevision: verified.Manifest.DeploymentRevision, BrokerCapability: verified.Manifest.BrokerCapability,
		MigrationSetDigest: verified.Manifest.MigrationSetDigest, RestartClass: verified.Manifest.RestartClass,
		RebootClass: verified.Manifest.RebootClass, RollbackClass: verified.Manifest.RollbackClass, BytesTotal: total,
		Revision: 1, CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
}

func (r Repository) db(ctx context.Context) (*gorm.DB, error) {
	if r.DB == nil || r.DB() == nil {
		return nil, errors.New("update repository is unavailable")
	}
	return r.DB().WithContext(ctx), nil
}

func (r Repository) lastApplied(ctx context.Context, channel release.Channel) (uint64, error) {
	db, err := r.db(ctx)
	if err != nil {
		return 0, err
	}
	var row model.UpdateReleaseState
	err = db.First(&row, "channel = ?", string(channel)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err == nil && !validPersistedReleaseState(row) {
		return 0, ErrRecoveryRequired
	}
	return row.LastAppliedSequence, err
}

func (r Repository) globalBlocker(ctx context.Context) (string, error) {
	db, err := r.db(ctx)
	if err != nil {
		return "", err
	}
	return operationcoordination.Blocker(ctx, db, operationcoordination.DomainUpdate), nil
}

func (r Repository) releaseState(ctx context.Context, channel release.Channel) (model.UpdateReleaseState, error) {
	db, err := r.db(ctx)
	if err != nil {
		return model.UpdateReleaseState{}, err
	}
	var row model.UpdateReleaseState
	err = db.First(&row, "channel = ?", string(channel)).Error
	return row, err
}

func (r Repository) saveVerified(ctx context.Context, verified release.Verified, channel release.Channel, available bool, observedAt int64) error {
	db, err := r.db(ctx)
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var activeCount int64
		if err := tx.Model(&model.UpdateOperation{}).
			Where("channel = ? AND manifest_digest <> ? AND state NOT IN ?", string(channel), verified.Digest,
				[]string{string(StateApplied), string(StateRolledBack), string(StateFailed)}).
			Count(&activeCount).Error; err != nil {
			return err
		}
		if activeCount > 0 {
			return ErrOperationConflict
		}
		var row model.UpdateReleaseState
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "channel = ?", string(channel)).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row.Channel = string(channel)
		} else if err != nil {
			return err
		} else if !validPersistedReleaseState(row) {
			return ErrRecoveryRequired
		}
		if verified.Manifest.Sequence < row.LastObservedSequence ||
			(verified.Manifest.Sequence == row.LastObservedSequence && row.ManifestDigest != "" && row.ManifestDigest != verified.Digest) {
			return ErrReleaseChanged
		}
		row.LastObservedSequence, row.LastVerifiedSequence = verified.Manifest.Sequence, verified.Manifest.Sequence
		row.ReleaseID, row.ManifestDigest, row.Version = verified.Manifest.ReleaseID, verified.Digest, verified.Manifest.Version
		row.SigningKeyID, row.ExpiresAt, row.UpdatedAt = verified.KeyID, verified.Manifest.ExpiresAt, observedAt
		return tx.Save(&row).Error
	})
}

func (r Repository) byIdempotency(ctx context.Context, key string) (model.UpdateOperation, error) {
	db, err := r.db(ctx)
	if err != nil {
		return model.UpdateOperation{}, err
	}
	var row model.UpdateOperation
	err = db.First(&row, "idempotency_key = ?", key).Error
	return row, err
}

func (r Repository) active(ctx context.Context) (model.UpdateOperation, error) {
	db, err := r.db(ctx)
	if err != nil {
		return model.UpdateOperation{}, err
	}
	var row model.UpdateOperation
	err = db.Where("state NOT IN ?", []string{string(StateApplied), string(StateRolledBack), string(StateFailed)}).
		Order("updated_at DESC").First(&row).Error
	return row, err
}

func (r Repository) activeOrRecovery(ctx context.Context) (model.UpdateOperation, error) {
	db, err := r.db(ctx)
	if err != nil {
		return model.UpdateOperation{}, err
	}
	var row model.UpdateOperation
	err = db.Where("state = ? OR state NOT IN ?", string(StateRecoveryRequired), []string{string(StateApplied), string(StateRolledBack), string(StateFailed)}).
		Order("updated_at DESC").First(&row).Error
	return row, err
}

func (r Repository) latest(ctx context.Context) (model.UpdateOperation, error) {
	db, err := r.db(ctx)
	if err != nil {
		return model.UpdateOperation{}, err
	}
	var row model.UpdateOperation
	err = db.Order("updated_at DESC, operation_id DESC").First(&row).Error
	return row, err
}

func (r Repository) operation(ctx context.Context, id string) (model.UpdateOperation, error) {
	db, err := r.db(ctx)
	if err != nil {
		return model.UpdateOperation{}, err
	}
	var row model.UpdateOperation
	err = db.First(&row, "operation_id = ?", id).Error
	return row, err
}

func (r Repository) timeline(ctx context.Context, id string, after uint64, limit int) ([]model.UpdateJournal, bool, error) {
	db, err := r.db(ctx)
	if err != nil {
		return nil, false, err
	}
	var rows []model.UpdateJournal
	err = db.Where("operation_id = ? AND sequence > ?", id, after).
		Order("sequence ASC").Limit(limit + 1).Find(&rows).Error
	if err != nil {
		return nil, false, err
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	return rows, truncated, nil
}

func (r Repository) create(ctx context.Context, operation model.UpdateOperation, event, reason string) error {
	db, err := r.db(ctx)
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&operation).Error; err != nil {
			return err
		}
		return tx.Create(journalFor(operation, event, reason)).Error
	})
}

func (r Repository) update(ctx context.Context, operation model.UpdateOperation, expectedRevision uint64, expectedState, event string) error {
	db, err := r.db(ctx)
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.UpdateOperation{}).Where("operation_id = ? AND revision = ? AND state = ?", operation.OperationID, expectedRevision, expectedState).
			Updates(map[string]any{"state": operation.State, "reason_code": operation.ReasonCode, "revision": operation.Revision,
				"rollback_available": operation.RollbackAvailable, "backup_ref": operation.BackupRef, "updated_at": operation.UpdatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRevisionMismatch
		}
		return tx.Create(journalFor(operation, event, operation.ReasonCode)).Error
	})
}

func (r Repository) progress(ctx context.Context, id string, bytes int64) error {
	db, err := r.db(ctx)
	if err != nil {
		return err
	}
	return db.Model(&model.UpdateOperation{}).Where("operation_id = ? AND bytes_completed < bytes_total", id).
		UpdateColumn("bytes_completed", gorm.Expr("MIN(bytes_total, bytes_completed + ?)", bytes)).Error
}

func (r Repository) completeApplied(ctx context.Context, operation model.UpdateOperation, expectedRevision uint64, expectedState, event string) error {
	db, err := r.db(ctx)
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var releaseState model.UpdateReleaseState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&releaseState, "channel = ?", operation.Channel).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrReleaseChanged
			}
			return err
		}
		if !validPersistedReleaseState(releaseState) || restoredReleaseState(releaseState) {
			return ErrReleaseChanged
		}
		if releaseState.LastVerifiedSequence != operation.Sequence ||
			releaseState.ManifestDigest != operation.ManifestDigest ||
			releaseState.LastAppliedSequence > operation.Sequence {
			return ErrReleaseChanged
		}
		result := tx.Model(&model.UpdateOperation{}).
			Where("operation_id = ? AND revision = ? AND state = ?", operation.OperationID, expectedRevision, expectedState).
			Updates(map[string]any{"state": operation.State, "reason_code": operation.ReasonCode, "revision": operation.Revision,
				"rollback_available": operation.RollbackAvailable, "backup_ref": operation.BackupRef, "updated_at": operation.UpdatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRevisionMismatch
		}
		result = tx.Model(&model.UpdateReleaseState{}).
			Where("channel = ? AND last_verified_sequence = ? AND manifest_digest = ? AND last_applied_sequence <= ?",
				operation.Channel, operation.Sequence, operation.ManifestDigest, operation.Sequence).
			Updates(map[string]any{"last_applied_sequence": operation.Sequence, "updated_at": operation.UpdatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrReleaseChanged
		}
		return tx.Create(journalFor(operation, event, operation.ReasonCode)).Error
	})
}

func journalFor(operation model.UpdateOperation, event, reason string) *model.UpdateJournal {
	return &model.UpdateJournal{OperationID: operation.OperationID, State: operation.State, Event: event, ReasonCode: reason,
		Revision: operation.Revision, SemanticHash: semanticDigest(struct {
			ID, State, Event, Reason string
			Revision                 uint64
		}{
			operation.OperationID, operation.State, event, reason, operation.Revision}), CreatedAt: operation.UpdatedAt}
}

func semanticDigest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func safeID(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._:@+-", char) {
			continue
		}
		return false
	}
	return true
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validPersistedReleaseState(row model.UpdateReleaseState) bool {
	if (row.Channel != string(release.ChannelMain) && row.Channel != string(release.ChannelBeta)) ||
		row.LastObservedSequence == 0 || row.LastAppliedSequence > row.LastObservedSequence ||
		!safeID(row.ReleaseID, 96) || row.UpdatedAt <= 0 || row.ExpiresAt <= 0 {
		return false
	}
	if _, ok := versionpolicy.CompareVersions(row.Version, row.Version); !ok {
		return false
	}
	if restoredReleaseState(row) {
		return true
	}
	return row.LastVerifiedSequence > 0 && row.LastVerifiedSequence == row.LastObservedSequence &&
		row.LastAppliedSequence <= row.LastVerifiedSequence && validDigest(row.ManifestDigest) &&
		safeID(row.SigningKeyID, 96)
}

func restoredReleaseState(row model.UpdateReleaseState) bool {
	return row.LastVerifiedSequence == 0 && row.ManifestDigest == "" && row.SigningKeyID == ""
}

func validPersistedOperation(operation model.UpdateOperation) bool {
	if !safeID(operation.OperationID, 96) || !strings.HasPrefix(operation.OperationID, "update-operation:") ||
		!safeID(operation.IdempotencyKey, 96) || operation.Revision == 0 || operation.Sequence == 0 ||
		!safeID(operation.ReleaseID, 96) || !validDigest(operation.ManifestDigest) ||
		!validDigest(operation.ArtifactSetDigest) || !validDigest(operation.DeploymentRevision) ||
		!safeID(operation.BrokerCapability, 96) || !validDigest(operation.MigrationSetDigest) ||
		(operation.BackupRef != "" && !validDigest(operation.BackupRef)) ||
		(operation.Channel != string(release.ChannelMain) && operation.Channel != string(release.ChannelBeta)) ||
		!safeID(operation.Platform, 24) || !safeID(operation.Arch, 24) ||
		(operation.BinaryProfile != "full" && operation.BinaryProfile != "core") ||
		(operation.RestartClass != "panel" && operation.RestartClass != "stack") ||
		(operation.RebootClass != "not-required" && operation.RebootClass != "operator-advisory") ||
		(operation.RollbackClass != "automatic" && operation.RollbackClass != "manual-recovery") ||
		operation.BytesTotal <= 0 || operation.BytesTotal > release.MaxReleaseSetBytes ||
		operation.BytesCompleted < 0 || operation.BytesCompleted > operation.BytesTotal ||
		operation.CreatedAt <= 0 || operation.UpdatedAt < operation.CreatedAt ||
		(operation.ReasonCode != "" && !safeID(operation.ReasonCode, 96)) {
		return false
	}
	if _, ok := versionpolicy.CompareVersions(operation.Version, operation.Version); !ok {
		return false
	}
	switch State(operation.State) {
	case StateDownloading, StateDownloaded, StateVerifying, StateVerified, StatePreflighting, StatePrepared,
		StateActivating, StateVerifyingActive, StateApplied, StateRollbackPending, StateRollingBack,
		StateRolledBack, StateFailed, StateRecoveryRequired:
		return true
	default:
		return false
	}
}

func operationRecoveryProjection(operation model.UpdateOperation) model.UpdateOperation {
	operation.State = string(StateRecoveryRequired)
	operation.ReasonCode = "update_operation_state_invalid"
	operation.RestoredUntrusted = true
	operation.RollbackAvailable = false
	return operation
}

func ReasonCode(err error) string {
	switch {
	case errors.Is(err, ErrSigningUnavailable):
		return "release_signing_unavailable"
	case errors.Is(err, ErrProviderUnavailable):
		return "update_provider_unavailable"
	case errors.Is(err, ErrRevisionMismatch):
		return "update_revision_mismatch"
	case errors.Is(err, ErrOperationConflict):
		return "update_operation_conflict"
	case errors.Is(err, ErrReleaseChanged):
		return "verified_release_changed"
	case errors.Is(err, ErrRecoveryRequired):
		return "update_recovery_required"
	case errors.Is(err, ErrResourcePressure):
		return "resource_pressure_blocks_update"
	default:
		return "update_operation_failed"
	}
}
