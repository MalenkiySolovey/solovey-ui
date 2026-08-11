package deployment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	"gorm.io/gorm"
)

type Preview struct {
	Posture            domain.Posture      `json:"posture"`
	Target             domain.Profile      `json:"target"`
	Doctor             domain.DoctorReport `json:"doctor"`
	ManagementRevision string              `json:"managementRevision"`
	Possible           bool                `json:"possible"`
	Reasons            []string            `json:"reasons,omitempty"`
	Revision           string              `json:"revision"`
}

type StartRequest struct {
	TargetProfile           domain.ProfileID `json:"targetProfile"`
	IdempotencyKey          string           `json:"idempotencyKey"`
	ExpectedPreviewRevision string           `json:"expectedPreviewRevision"`
	ExpectedPostureRevision string           `json:"expectedPostureRevision"`
	Acknowledged            bool             `json:"acknowledged"`
}

type ConfirmRequest struct {
	OperationID      string `json:"operationId"`
	ExpectedRevision uint64 `json:"expectedRevision"`
}

type AuditEvent struct {
	Event       string
	OperationID string
	State       domain.OperationState
	Revision    uint64
	Reason      string
}

type Manager struct {
	Repository Repository
	Provider   Provider
	Now        func() time.Time
	Random     io.Reader
	Audit      func(context.Context, AuditEvent)
	Management func(context.Context, time.Time) ManagementPreservation
	Health     func(context.Context, time.Time) RuntimeHealth
	mu         sync.Mutex
}

func NewManager(repository Repository, provider Provider) *Manager {
	if provider == nil {
		provider = UnavailableProvider{}
	}
	return &Manager{Repository: repository, Provider: provider, Now: time.Now, Random: rand.Reader}
}

func DefaultManager() *Manager {
	manager := NewManager(Repository{DB: dbsqlite.DB}, RuntimeProvider())
	manager.Management = productionManagementPreservation
	return manager
}

var (
	sharedOnce sync.Once
	shared     *Manager
)

func Shared() *Manager {
	sharedOnce.Do(func() { shared = DefaultManager() })
	return shared
}

func (m *Manager) Capabilities(ctx context.Context) domain.Capabilities {
	if m == nil || m.Provider == nil {
		return UnavailableProvider{}.Capabilities(ctx)
	}
	return m.Provider.Capabilities(ctx)
}

func (m *Manager) Status(ctx context.Context) (domain.Posture, error) {
	if m == nil || m.Provider == nil {
		return domain.Posture{}, ErrProviderUnavailable
	}
	posture, err := m.Provider.Observe(ctx)
	if err != nil {
		return domain.Posture{}, err
	}
	if err := posture.ValidateProjection(m.now()); err != nil {
		return domain.Posture{}, err
	}
	_ = m.Repository.SavePosture(ctx, posture, posture.Validate(m.now()) == nil)
	return posture, nil
}

func (m *Manager) Doctor(ctx context.Context) (domain.DoctorReport, error) {
	if m == nil || m.Provider == nil {
		return domain.DoctorReport{}, ErrProviderUnavailable
	}
	report, err := m.Provider.Doctor(ctx)
	if err != nil {
		return domain.DoctorReport{}, err
	}
	if report.Validate(m.now()) != nil {
		return domain.DoctorReport{}, ErrRevisionMismatch
	}
	if report.Posture != nil {
		_ = m.Repository.SavePosture(ctx, *report.Posture, report.Healthy && report.Posture.Validate(m.now()) == nil)
	}
	if state, stateErr := m.Repository.State(ctx); stateErr == nil {
		report.Desired = domain.ProfileID(state.DesiredProfile)
		report.Generated = domain.ProfileID(state.GeneratedProfile)
		report.Installed = domain.ProfileID(state.InstalledProfile)
		report.Active = domain.ProfileID(state.ActiveProfile)
		report.Verified = domain.ProfileID(state.VerifiedProfile)
		switch {
		case state.GeneratedProfile != "" && state.GeneratedProfile != state.InstalledProfile:
			report.State = "GENERATED_NOT_INSTALLED"
		case state.InstalledProfile != "" && state.InstalledProfile != state.ActiveProfile:
			report.State = "INSTALLED_NOT_ACTIVE"
		case !state.Trusted || state.ActiveProfile != state.VerifiedProfile:
			report.State = "ACTIVE_NOT_VERIFIED"
		}
	}
	if recovery, recoveryErr := m.Repository.Recovery(ctx); recoveryErr == nil && (recovery.State == domain.StateManualRecoveryRequired || recovery.RestoredUntrusted) {
		report.State = "RECOVERY_REQUIRED"
		report.Healthy = false
	}
	report.Revision = ""
	report.Revision = domain.Revision(report)
	_ = m.Repository.SaveDoctor(ctx, report)
	return report, nil
}

func (m *Manager) Preview(ctx context.Context, targetID domain.ProfileID, acknowledged bool) (Preview, error) {
	posture, err := m.Status(ctx)
	if err != nil {
		return Preview{}, err
	}
	target, ok := domain.Lookup(targetID)
	if !ok {
		return Preview{}, ErrUnsafeMigration
	}
	report, err := m.Doctor(ctx)
	if err != nil {
		return Preview{}, err
	}
	management := m.management(ctx)
	preview := Preview{Posture: posture, Target: target, Doctor: report, ManagementRevision: management.Revision}
	if !management.Ready {
		preview.Reasons = append(preview.Reasons, management.Reasons...)
	}
	health := m.health(ctx)
	if !health.Ready {
		preview.Reasons = append(preview.Reasons, health.Reasons...)
	}
	if posture.Runtime != domain.RuntimeNative || target.Runtime != domain.RuntimeNative {
		preview.Reasons = append(preview.Reasons, "cross_runtime_migration_not_supported")
	}
	if target.ID == domain.NativeLegacyRoot {
		preview.Reasons = append(preview.Reasons, "legacy_root_is_rollback_only")
	}
	if target.ID == domain.NativeNetworkAdvanced {
		// The current core is in-process. Granting its network capabilities to
		// that process would also grant them to the panel, which is forbidden.
		preview.Reasons = append(preview.Reasons, "separate_network_runtime_unavailable")
	}
	if posture.Profile == target.ID {
		preview.Reasons = append(preview.Reasons, "profile_already_active")
	}
	if !report.Healthy {
		preview.Reasons = append(preview.Reasons, "deployment_doctor_has_critical_findings")
	}
	if m.Capabilities(ctx).Migrate != domain.Available {
		preview.Reasons = append(preview.Reasons, "migration_provider_unavailable")
	}
	if !acknowledged {
		preview.Reasons = append(preview.Reasons, "operator_acknowledgement_missing")
	}
	preview.Reasons = unique(preview.Reasons)
	preview.Possible = len(preview.Reasons) == 0
	preview.Revision = domain.Revision(struct {
		Posture    string
		Target     string
		Doctor     string
		Management string
		Health     string
		Reasons    []string
	}{posture.Revision, target.Revision, report.Revision, management.Revision, health.Revision, preview.Reasons})
	return preview, nil
}

func (m *Manager) Start(ctx context.Context, request StartRequest) (domain.Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if request.IdempotencyKey == "" {
		return domain.Operation{}, ErrUnsafeMigration
	}
	if replay, err := m.Repository.ByIdempotency(ctx, request.IdempotencyKey); err == nil {
		if replay.TargetProfile != request.TargetProfile {
			return domain.Operation{}, ErrOperationConflict
		}
		return replay, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Operation{}, err
	}
	preview, err := m.Preview(ctx, request.TargetProfile, request.Acknowledged)
	if err != nil || !preview.Possible || preview.Revision != request.ExpectedPreviewRevision || preview.Posture.Revision != request.ExpectedPostureRevision {
		return domain.Operation{}, ErrRevisionMismatch
	}
	now := m.now()
	id, err := m.randomID("deployment-operation:", 16)
	if err != nil {
		return domain.Operation{}, err
	}
	operation := domain.Operation{Schema: domain.SchemaV1, OperationID: id, IdempotencyKey: request.IdempotencyKey,
		State: domain.StateDraft, FromProfile: preview.Posture.Profile, TargetProfile: request.TargetProfile,
		ExpectedPosture: preview.Posture.Revision, ExpectedManagement: preview.ManagementRevision,
		Revision: 1, CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
	operation.BindingRevision = domain.OperationBinding(operation)
	if err := m.Repository.Admit(ctx, operation, "draft_created"); err != nil {
		return domain.Operation{}, err
	}
	m.emit(ctx, operation, "draft_created", "")
	operation, err = m.advance(ctx, operation, domain.StatePreflighted, "preflight_completed", "")
	if err != nil {
		return domain.Operation{}, err
	}
	checkpoint, err := m.Provider.Prepare(ctx, m.fence(operation), operation.TargetProfile)
	if err != nil || len(checkpoint) != 64 {
		return m.manual(ctx, operation, "prepare_failed")
	}
	operation.CheckpointRef = checkpoint
	operation, err = m.advance(ctx, operation, domain.StateApplying, "checkpoint_persisted", "")
	if err != nil {
		return domain.Operation{}, err
	}
	if management := m.management(ctx); !management.Ready || management.Revision != operation.ExpectedManagement {
		return m.rollback(ctx, operation, "management_preservation_changed")
	}
	if health := m.health(ctx); !health.Ready {
		return m.rollback(ctx, operation, "runtime_health_changed")
	}
	// A root-to-unprivileged handoff may terminate the calling panel process.
	// The APPLYING row is durable first; startup reconciliation completes it.
	if err := m.Provider.Apply(ctx, m.fence(operation), operation.TargetProfile, operation.CheckpointRef); err != nil {
		return m.rollback(ctx, operation, "apply_failed")
	}
	posture, err := m.Provider.Verify(ctx, m.fence(operation), operation.TargetProfile, operation.CheckpointRef)
	if err != nil || posture.Profile != operation.TargetProfile {
		return m.rollback(ctx, operation, "verification_failed")
	}
	_ = m.Repository.SavePosture(ctx, posture, true)
	return m.advance(ctx, operation, domain.StateVerifying, "target_verified", "")
}

func (m *Manager) Confirm(ctx context.Context, request ConfirmRequest) (domain.Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	operation, err := m.Repository.ByID(ctx, request.OperationID)
	if err != nil || operation.State != domain.StateVerifying || operation.Revision != request.ExpectedRevision || operation.RestoredUntrusted {
		return domain.Operation{}, ErrOperationConflict
	}
	if management := m.management(ctx); !management.Ready || management.Revision != operation.ExpectedManagement {
		return m.rollback(ctx, operation, "management_preservation_changed")
	}
	if health := m.health(ctx); !health.Ready {
		return m.rollback(ctx, operation, "runtime_health_failed")
	}
	posture, err := m.Provider.Verify(ctx, m.fence(operation), operation.TargetProfile, operation.CheckpointRef)
	if err != nil || posture.Profile != operation.TargetProfile || posture.Validate(m.now()) != nil {
		return m.rollback(ctx, operation, "confirm_verification_failed")
	}
	operation.ReconciledAt = m.now().Unix()
	_ = m.Repository.SavePosture(ctx, posture, true)
	return m.advance(ctx, operation, domain.StateCommitted, "migration_committed", "")
}

func (m *Manager) Rollback(ctx context.Context, request ConfirmRequest) (domain.Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	operation, err := m.Repository.ByID(ctx, request.OperationID)
	if err != nil || operation.Revision != request.ExpectedRevision || operation.State.Terminal() {
		return domain.Operation{}, ErrOperationConflict
	}
	return m.rollback(ctx, operation, "operator_requested")
}

func (m *Manager) Operation(ctx context.Context, id string) (domain.Operation, error) {
	return m.Repository.ByID(ctx, id)
}

func (m *Manager) Timeline(ctx context.Context, id string) ([]model.DeploymentJournal, error) {
	return m.Repository.Timeline(ctx, id)
}

func (m *Manager) ReconcileStartup(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	operation, err := m.Repository.Active(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if operation.RestoredUntrusted || operation.CheckpointRef == "" {
		_, err = m.manual(ctx, operation, "restored_or_checkpoint_missing")
		return err
	}
	if management := m.management(ctx); !management.Ready || management.Revision != operation.ExpectedManagement {
		_, err = m.rollback(ctx, operation, "management_preservation_changed")
		return err
	}
	if operation.State == domain.StateApplying || operation.State == domain.StateVerifying {
		posture, verifyErr := m.Provider.Verify(ctx, m.fence(operation), operation.TargetProfile, operation.CheckpointRef)
		if verifyErr == nil && posture.Profile == operation.TargetProfile && posture.Validate(m.now()) == nil {
			_ = m.Repository.SavePosture(ctx, posture, true)
			if operation.State == domain.StateApplying {
				_, err = m.advance(ctx, operation, domain.StateVerifying, "startup_target_verified", "")
			}
			return err
		}
		_, err = m.rollback(ctx, operation, "startup_verification_failed")
		return err
	}
	_, err = m.rollback(ctx, operation, "startup_interrupted")
	return err
}

func (m *Manager) rollback(ctx context.Context, operation domain.Operation, reason string) (domain.Operation, error) {
	if operation.CheckpointRef == "" {
		return m.manual(ctx, operation, "rollback_checkpoint_missing")
	}
	if operation.State != domain.StateRollbackPending {
		var err error
		operation, err = m.advance(ctx, operation, domain.StateRollbackPending, "rollback_pending", reason)
		if err != nil {
			return domain.Operation{}, err
		}
	}
	posture, err := m.Provider.Rollback(ctx, m.fence(operation), operation.FromProfile, operation.CheckpointRef)
	if err != nil || posture.Profile != operation.FromProfile || posture.Validate(m.now()) != nil {
		return m.manual(ctx, operation, "rollback_verification_failed")
	}
	operation.ReconciledAt = m.now().Unix()
	_ = m.Repository.SavePosture(ctx, posture, true)
	return m.advance(ctx, operation, domain.StateRolledBack, "rollback_verified", reason)
}

func (m *Manager) manual(ctx context.Context, operation domain.Operation, reason string) (domain.Operation, error) {
	operation.Reasons = unique(append(operation.Reasons, reason))
	result, err := m.advance(ctx, operation, domain.StateManualRecoveryRequired, "manual_recovery_required", reason)
	if err != nil {
		return domain.Operation{}, err
	}
	return result, ErrUnsafeMigration
}

func (m *Manager) advance(ctx context.Context, operation domain.Operation, state domain.OperationState, event, reason string) (domain.Operation, error) {
	expectedRevision, expectedState := operation.Revision, operation.State
	operation.State = state
	operation.Revision++
	operation.UpdatedAt = m.now().Unix()
	if err := m.Repository.Update(ctx, operation, expectedRevision, expectedState, event, reason); err != nil {
		return domain.Operation{}, err
	}
	m.emit(ctx, operation, event, reason)
	return operation, nil
}

func (m *Manager) fence(operation domain.Operation) FenceV1 {
	return FenceV1{OperationID: operation.OperationID, Revision: operation.Revision,
		Token: domain.Revision(struct {
			Operation string
			Revision  uint64
			Binding   string
		}{operation.OperationID, operation.Revision, operation.BindingRevision}),
		ExpectedPosture: operation.ExpectedPosture, DeadlineAt: m.now().Add(MaxProviderDuration).Unix()}
}

func (m *Manager) now() time.Time {
	if m != nil && m.Now != nil {
		return m.Now().UTC().Truncate(time.Second)
	}
	return time.Now().UTC().Truncate(time.Second)
}

func (m *Manager) management(ctx context.Context) ManagementPreservation {
	if m == nil || m.Management == nil {
		result := ManagementPreservation{EvidenceRevision: domain.Revision("management-preservation-unavailable"), Reasons: []string{"management_preservation_unavailable"}}
		result.Revision = domain.Revision(result)
		return result
	}
	result := m.Management(ctx, m.now())
	result.Reasons = unique(result.Reasons)
	copy := result
	copy.Revision = ""
	_, evidenceErr := hex.DecodeString(result.EvidenceRevision)
	if len(result.EvidenceRevision) != 64 || evidenceErr != nil || result.Revision == "" || result.Revision != domain.Revision(copy) {
		result = ManagementPreservation{EvidenceRevision: domain.Revision("management-preservation-invalid"), Reasons: []string{"management_preservation_invalid"}}
		result.Revision = domain.Revision(result)
	}
	return result
}

func (m *Manager) health(ctx context.Context) RuntimeHealth {
	if m == nil || m.Health == nil {
		result := RuntimeHealth{EvidenceRevision: domain.Revision("runtime-health-unavailable"), Reasons: []string{"runtime_health_unavailable"}}
		result.Revision = domain.Revision(result)
		return result
	}
	result := m.Health(ctx, m.now())
	result.Reasons = unique(result.Reasons)
	copy := result
	copy.Revision = ""
	_, evidenceErr := hex.DecodeString(result.EvidenceRevision)
	if len(result.EvidenceRevision) != 64 || evidenceErr != nil || result.Revision == "" || result.Revision != domain.Revision(copy) {
		result = RuntimeHealth{EvidenceRevision: domain.Revision("runtime-health-invalid"), Reasons: []string{"runtime_health_invalid"}}
		result.Revision = domain.Revision(result)
	}
	return result
}

func (m *Manager) randomID(prefix string, length int) (string, error) {
	reader := m.Random
	if reader == nil {
		reader = rand.Reader
	}
	buffer := make([]byte, length)
	if _, err := io.ReadFull(reader, buffer); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buffer), nil
}

func (m *Manager) emit(ctx context.Context, operation domain.Operation, event, reason string) {
	if m.Audit != nil {
		m.Audit(ctx, AuditEvent{Event: event, OperationID: operation.OperationID, State: operation.State, Revision: operation.Revision, Reason: reason})
	}
}

func unique(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
