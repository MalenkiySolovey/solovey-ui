package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	NativeFallbackOperationSchemaV1 = "solovey-ui/native-fallback-operation/v1"
	NativeFallbackMirrorSchemaV1    = "solovey-ui/provider-reservation-mirror/v1"
	NativeRecoveryRestoredUntrusted = "restored_state_untrusted"

	NativeWorkflowPreparing         = "PREPARING"
	NativeWorkflowPrepared          = "PREPARED"
	NativeWorkflowApplying          = "APPLYING"
	NativeWorkflowHealth            = "HEALTH"
	NativeWorkflowApplied           = "APPLIED"
	NativeWorkflowRollingBack       = "ROLLING_BACK"
	NativeWorkflowRolledBack        = "ROLLED_BACK"
	NativeWorkflowRollbackFailed    = "ROLLBACK_FAILED"
	NativeWorkflowReconcileRequired = "RECONCILE_REQUIRED"
	NativeWorkflowCancelled         = "CANCELLED"
)

type NativeJournalStage string

const (
	NativeJournalReservation       NativeJournalStage = "reservation"
	NativeJournalPrepared          NativeJournalStage = "prepared"
	NativeJournalApplying          NativeJournalStage = "applying"
	NativeJournalFenced            NativeJournalStage = "fenced"
	NativeJournalHealth            NativeJournalStage = "health"
	NativeJournalApplied           NativeJournalStage = "applied"
	NativeJournalRollingBack       NativeJournalStage = "rolling_back"
	NativeJournalRolledBack        NativeJournalStage = "rolled_back"
	NativeJournalCancelled         NativeJournalStage = "cancelled"
	NativeJournalRollbackFailed    NativeJournalStage = "rollback_failed"
	NativeJournalReconcileRequired NativeJournalStage = "reconcile_required"
	NativeJournalReverified        NativeJournalStage = "reverified"
)

type NativeFallbackJournalUpdate struct {
	OperationID                string
	ExpectedRevision           int
	Stage                      NativeJournalStage
	Reservation                *neutralfallback.ProviderTargetReservationV1
	CheckpointID               string
	CheckpointDigest           string
	CheckpointReleaseProof     string
	CheckpointReleased         bool
	ArtifactRevision           string
	ArtifactManifestDigest     string
	AfterConfigurationRevision string
	ExpectedEffectiveRevision  string
	EffectiveRevision          string
	ManagerGeneration          uint64
	HealthResultRevision       string
	HealthFactsJSON            json.RawMessage
	RecoveryClassification     string
	ReasonCodes                []domain.NativeFallbackReasonCode
	RecoveryBundleJSON         json.RawMessage
	Now                        time.Time
}

func (r *Repository) CreateNativeFallbackOperation(ctx context.Context, model NativeFallbackOperationModel) (NativeFallbackOperationModel, error) {
	if r == nil || r.db == nil {
		return NativeFallbackOperationModel{}, errors.New("server-protection repository is not initialized")
	}
	if model.Schema != NativeFallbackOperationSchemaV1 || !domain.ValidContractID(model.OperationID, 128) ||
		!domain.ValidContractID(model.ResourceID, 256) || model.InboundDatabaseID == 0 || !domain.ValidSHA256(model.PlanID) ||
		model.PlanID != model.PlanDigest || !domain.ValidSHA256(model.RuntimeIdentityRevision) ||
		!domain.ValidSHA256(model.CapabilityResolverRevision) || !domain.ValidSHA256(model.BeforeConfigurationRevision) ||
		!domain.ValidSHA256(model.ExpectedAfterRevision) || !domain.ValidSHA256(model.BeforeEffectiveRevision) ||
		!domain.ValidSHA256(model.TargetRevision) || !domain.ValidSHA256(model.EndpointRevision) ||
		!domain.ValidSHA256(model.HealthRevision) || !domain.ValidSHA256(model.CapacityRevision) ||
		len(model.PlanJSON) == 0 || len(model.PlanJSON) > 64<<10 || len(model.TargetReferenceJSON) == 0 || len(model.TargetReferenceJSON) > 8<<10 {
		return NativeFallbackOperationModel{}, errors.New("native fallback operation contract is invalid")
	}
	var plan domain.NativeFallbackPlanV1
	var reference neutralfallback.FallbackTargetReferenceV2
	if json.Unmarshal(model.PlanJSON, &plan) != nil || plan.Validate() != nil || plan.PlanDigest != model.PlanDigest ||
		json.Unmarshal(model.TargetReferenceJSON, &reference) != nil || reference.Validate() != nil || reference != plan.Target.Reference {
		return NativeFallbackOperationModel{}, errors.New("native fallback operation plan binding is invalid")
	}
	now := model.CreatedAt
	if now <= 0 {
		now = time.Now().UTC().Unix()
	}
	model.Revision = 1
	model.WorkflowState = NativeWorkflowPreparing
	model.HealthFactsJSON = []byte("{}")
	model.ReasonCodesJSON = []byte("[]")
	model.RecoveryBundleJSON = []byte("{}")
	model.CreatedAt, model.UpdatedAt = now, now
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var guard NativeFallbackStateModel
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("resource_id = ?", model.ResourceID).First(&guard).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			switch domain.NativeFallbackActualState(guard.ActualState) {
			case domain.NativeActualNotApplied, domain.NativeActualRolledBack:
			default:
				return ErrOperationConflict
			}
		}
		return tx.Create(&model).Error
	})
	return model, err
}

func (r *Repository) NativeFallbackOperation(ctx context.Context, operationID string) (NativeFallbackOperationModel, error) {
	if r == nil || r.db == nil {
		return NativeFallbackOperationModel{}, errors.New("server-protection repository is not initialized")
	}
	var item NativeFallbackOperationModel
	err := r.db.WithContext(ctx).Where("operation_id = ?", operationID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NativeFallbackOperationModel{}, ErrRecordNotFound
	}
	return item, err
}

func (r *Repository) ListNativeFallbackOperations(ctx context.Context, states []string) ([]NativeFallbackOperationModel, error) {
	query := r.db.WithContext(ctx).Model(&NativeFallbackOperationModel{})
	if len(states) != 0 {
		query = query.Where("workflow_state IN ?", states)
	}
	var items []NativeFallbackOperationModel
	return items, query.Order("created_at ASC, id ASC").Find(&items).Error
}

func (r *Repository) ReservationMirror(ctx context.Context, operationID string) (FallbackTargetLeaseModel, error) {
	var item FallbackTargetLeaseModel
	err := r.db.WithContext(ctx).Where("operation_id = ?", operationID).Order("id DESC").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return FallbackTargetLeaseModel{}, ErrRecordNotFound
	}
	return item, err
}

// AdvanceNativeFallbackOperation is a typed stage transition, not a generic
// state writer. Every legal edge, required proof, revision CAS, native state
// projection, and reservation mirror update is enforced in one transaction.
func (r *Repository) AdvanceNativeFallbackOperation(ctx context.Context, update NativeFallbackJournalUpdate) (NativeFallbackOperationModel, error) {
	if r == nil || r.db == nil || !domain.ValidContractID(update.OperationID, 128) || update.ExpectedRevision <= 0 {
		return NativeFallbackOperationModel{}, errors.New("native fallback journal update is invalid")
	}
	if update.Now.IsZero() {
		update.Now = time.Now().UTC()
	}
	update.Now = update.Now.UTC().Truncate(time.Second)
	var result NativeFallbackOperationModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current NativeFallbackOperationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("operation_id = ?", update.OperationID).First(&current).Error; err != nil {
			return err
		}
		if current.Revision != update.ExpectedRevision {
			return ErrRevisionConflict
		}
		nextState, actualState, err := applyNativeJournalStage(&current, update)
		if err != nil {
			return err
		}
		current.WorkflowState = nextState
		current.Revision++
		current.UpdatedAt = update.Now.Unix()
		if update.Reservation != nil {
			if err := saveReservationMirrorTx(tx, current, *update.Reservation); err != nil {
				return err
			}
			current.ProviderReservationID = update.Reservation.ReservationID
			current.ProviderReservationRevision = update.Reservation.ReservationRevision
		}
		write := tx.Model(&NativeFallbackOperationModel{}).Where("operation_id = ? AND revision = ?", current.OperationID, update.ExpectedRevision).Updates(nativeOperationAssignments(current))
		if write.Error != nil {
			return write.Error
		}
		if write.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		if actualState != "" {
			if err := writeNativeStateTx(tx, current, actualState, update.ReasonCodes, update.Now); err != nil {
				return err
			}
		}
		result = current
		return nil
	})
	return result, err
}

func applyNativeJournalStage(current *NativeFallbackOperationModel, update NativeFallbackJournalUpdate) (string, domain.NativeFallbackActualState, error) {
	if current == nil {
		return "", "", ErrRevisionConflict
	}
	reasons, err := json.Marshal(domain.CanonicalNativeFallbackReasons(update.ReasonCodes))
	if err != nil {
		return "", "", err
	}
	if len(update.RecoveryBundleJSON) > 16<<10 || !safeRecoveryBundle(update.RecoveryBundleJSON) {
		return "", "", errors.New("native fallback recovery bundle is unsafe")
	}
	switch update.Stage {
	case NativeJournalReservation:
		if current.WorkflowState != NativeWorkflowPreparing || update.Reservation == nil || update.Reservation.State != neutralfallback.ReservationReserved || update.Reservation.HolderID != current.OperationID {
			return "", "", ErrOperationConflict
		}
		return NativeWorkflowPreparing, "", nil
	case NativeJournalPrepared:
		if current.WorkflowState != NativeWorkflowPreparing || current.ProviderReservationID == "" || !domain.ValidContractID(update.CheckpointID, 128) ||
			!domain.ValidSHA256(update.CheckpointDigest) || !domain.ValidSHA256(update.CheckpointReleaseProof) || !domain.ValidContractID(update.ArtifactRevision, 128) || !domain.ValidSHA256(update.ArtifactManifestDigest) {
			return "", "", ErrOperationConflict
		}
		current.CoreCheckpointID, current.CoreCheckpointDigest, current.CheckpointReleaseProof = update.CheckpointID, update.CheckpointDigest, update.CheckpointReleaseProof
		current.ArtifactRevision, current.ArtifactManifestDigest = update.ArtifactRevision, update.ArtifactManifestDigest
		at := update.Now.Unix()
		current.PreparedAt = &at
		return NativeWorkflowPrepared, domain.NativeActualPrepared, nil
	case NativeJournalApplying:
		if current.WorkflowState != NativeWorkflowPrepared || current.MutationMarkedAt != nil {
			return "", "", ErrOperationConflict
		}
		at := update.Now.Unix()
		current.MutationMarkedAt = &at
		return NativeWorkflowApplying, domain.NativeActualApplying, nil
	case NativeJournalFenced:
		if current.WorkflowState != NativeWorkflowApplying || current.MutationMarkedAt == nil || update.Reservation == nil || update.Reservation.State != neutralfallback.ReservationMutationPending {
			return "", "", ErrOperationConflict
		}
		return NativeWorkflowApplying, "", nil
	case NativeJournalHealth:
		if current.WorkflowState != NativeWorkflowApplying || !domain.ValidSHA256(update.AfterConfigurationRevision) || update.AfterConfigurationRevision != current.ExpectedAfterRevision ||
			!domain.ValidSHA256(update.ExpectedEffectiveRevision) || update.ManagerGeneration == 0 {
			return "", "", ErrOperationConflict
		}
		current.AfterConfigurationRevision, current.ExpectedEffectiveRevision = update.AfterConfigurationRevision, update.ExpectedEffectiveRevision
		current.ManagerGeneration = update.ManagerGeneration
		return NativeWorkflowHealth, domain.NativeActualHealth, nil
	case NativeJournalApplied, NativeJournalReverified:
		reverifiedRestored := update.Stage == NativeJournalReverified && nativeRestoredAppliedCanReverify(current)
		if (update.Stage == NativeJournalApplied && current.WorkflowState != NativeWorkflowHealth) ||
			(update.Stage == NativeJournalReverified && current.WorkflowState != NativeWorkflowApplied && !reverifiedRestored) ||
			update.Reservation == nil || update.Reservation.State != neutralfallback.ReservationActive || !domain.ValidSHA256(update.EffectiveRevision) ||
			!domain.ValidSHA256(update.HealthResultRevision) || len(update.HealthFactsJSON) == 0 || len(update.HealthFactsJSON) > 16<<10 {
			return "", "", ErrOperationConflict
		}
		current.EffectiveRevision, current.HealthResultRevision, current.HealthFactsJSON = update.EffectiveRevision, update.HealthResultRevision, append([]byte(nil), update.HealthFactsJSON...)
		current.ManagerGeneration = update.ManagerGeneration
		current.ReasonCodesJSON = reasons
		if reverifiedRestored {
			current.RecoveryClassification = ""
			current.RecoveryBundleJSON = []byte("{}")
		}
		at := update.Now.Unix()
		current.AppliedAt = &at
		return NativeWorkflowApplied, domain.NativeActualApplied, nil
	case NativeJournalRollingBack:
		if current.RollbackAttemptCount != 0 || (current.WorkflowState != NativeWorkflowApplying && current.WorkflowState != NativeWorkflowHealth && current.WorkflowState != NativeWorkflowApplied) {
			return "", "", ErrOperationConflict
		}
		current.RollbackAttemptCount = 1
		current.RecoveryClassification = boundedCode(update.RecoveryClassification)
		current.ReasonCodesJSON = reasons
		return NativeWorkflowRollingBack, domain.NativeActualRollingBack, nil
	case NativeJournalRolledBack:
		if current.WorkflowState != NativeWorkflowRollingBack || update.Reservation == nil || update.Reservation.State != neutralfallback.ReservationReleased || !update.CheckpointReleased {
			return "", "", ErrOperationConflict
		}
		at := update.Now.Unix()
		current.CoreCheckpointReleasedAt = &at
		current.RolledBackAt = &at
		current.ReasonCodesJSON = reasons
		return NativeWorkflowRolledBack, domain.NativeActualRolledBack, nil
	case NativeJournalCancelled:
		if (current.WorkflowState != NativeWorkflowPreparing && current.WorkflowState != NativeWorkflowPrepared) || current.MutationMarkedAt != nil ||
			(current.ProviderReservationID != "" && (update.Reservation == nil || update.Reservation.State != neutralfallback.ReservationReleased)) ||
			(current.CoreCheckpointID != "" && !update.CheckpointReleased) {
			return "", "", ErrOperationConflict
		}
		if update.CheckpointReleased {
			at := update.Now.Unix()
			current.CoreCheckpointReleasedAt = &at
		}
		current.ReasonCodesJSON = reasons
		return NativeWorkflowCancelled, domain.NativeActualNotApplied, nil
	case NativeJournalRollbackFailed, NativeJournalReconcileRequired:
		if current.WorkflowState == NativeWorkflowRolledBack || current.WorkflowState == NativeWorkflowCancelled {
			return "", "", ErrOperationConflict
		}
		current.RecoveryClassification = boundedCode(update.RecoveryClassification)
		current.ReasonCodesJSON = reasons
		current.RecoveryBundleJSON = append([]byte(nil), update.RecoveryBundleJSON...)
		if update.Stage == NativeJournalRollbackFailed {
			return NativeWorkflowRollbackFailed, domain.NativeActualRollbackFailed, nil
		}
		return NativeWorkflowReconcileRequired, domain.NativeActualReconcileRequired, nil
	default:
		return "", "", ErrOperationConflict
	}
}

func nativeRestoredAppliedCanReverify(current *NativeFallbackOperationModel) bool {
	return current != nil && current.WorkflowState == NativeWorkflowReconcileRequired &&
		current.RecoveryClassification == NativeRecoveryRestoredUntrusted && current.MutationMarkedAt != nil && current.AppliedAt != nil &&
		current.RollbackAttemptCount == 0 && current.AfterConfigurationRevision == current.ExpectedAfterRevision &&
		domain.ValidSHA256(current.ExpectedAfterRevision) && domain.ValidSHA256(current.ExpectedEffectiveRevision) &&
		domain.ValidSHA256(current.EffectiveRevision) && domain.ValidSHA256(current.HealthResultRevision) &&
		domain.ValidContractID(current.ProviderReservationID, 128) && domain.ValidContractID(current.ProviderReservationRevision, 128) &&
		domain.ValidContractID(current.CoreCheckpointID, 128) && domain.ValidSHA256(current.CoreCheckpointDigest)
}

func writeNativeStateTx(tx *gorm.DB, operation NativeFallbackOperationModel, actual domain.NativeFallbackActualState, reasons []domain.NativeFallbackReasonCode, now time.Time) error {
	var plan domain.NativeFallbackPlanV1
	if json.Unmarshal(operation.PlanJSON, &plan) != nil || plan.Validate() != nil {
		return errors.New("native fallback operation plan is invalid")
	}
	reasonJSON, _ := json.Marshal(domain.CanonicalNativeFallbackReasons(reasons))
	created := operation.CreatedAt
	var existing NativeFallbackStateModel
	if err := tx.Where("resource_id = ?", operation.ResourceID).First(&existing).Error; err == nil {
		created = existing.CreatedAt
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	row := NativeFallbackStateModel{
		ResourceID: operation.ResourceID, Schema: domain.NativeFallbackStateSchemaV1, InboundDatabaseID: operation.InboundDatabaseID,
		LatestPlanID: operation.PlanID, LatestPlanDigest: operation.PlanDigest, RuntimeIdentityRevision: operation.RuntimeIdentityRevision,
		CapabilityResolverRevision: operation.CapabilityResolverRevision, BeforeConfigurationRevision: operation.BeforeConfigurationRevision,
		AfterConfigurationRevision: operation.AfterConfigurationRevision, EffectiveRevision: operation.EffectiveRevision,
		TargetRevision: operation.TargetRevision, ProviderRevision: operation.ProviderRevision, EndpointRevision: operation.EndpointRevision,
		PublishRevision: operation.PublishRevision, HealthRevision: operation.HealthRevision, CapacityRevision: operation.CapacityRevision,
		ProviderReservationID: operation.ProviderReservationID, ProviderReservationRevision: operation.ProviderReservationRevision,
		OperationID: operation.OperationID, OperationRevision: strconv.Itoa(operation.Revision), DesiredState: string(domain.NativeFallbackDesired),
		SelectedVariant: string(plan.SelectedVariant), ActualState: string(actual), LastGoodCheckpointID: operation.CoreCheckpointID,
		LastGoodCheckpointDigest: operation.CoreCheckpointDigest, ReasonCodesJSON: reasonJSON, CreatedAt: created, UpdatedAt: now.Unix(),
	}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "resource_id"}}, UpdateAll: true}).Create(&row).Error
}

func saveReservationMirrorTx(tx *gorm.DB, operation NativeFallbackOperationModel, reservation neutralfallback.ProviderTargetReservationV1) error {
	if reservation.Validate() != nil || reservation.HolderID != operation.OperationID || reservation.ExactTargetReference.ProviderID == "" {
		return errors.New("provider reservation mirror input is invalid")
	}
	reasons, _ := json.Marshal(reservation.ReasonCodes)
	reference := reservation.ExactTargetReference
	row := FallbackTargetLeaseModel{
		Schema: NativeFallbackMirrorSchemaV1, LeaseID: reservation.ReservationID, HolderID: reservation.HolderID,
		StrategyPlanID: operation.PlanID, OperationID: operation.OperationID, ResourceID: operation.ResourceID,
		ProviderReservationID: reservation.ReservationID, ProviderReservationRevision: reservation.ReservationRevision,
		ProviderID: reference.ProviderID, TargetID: reference.TargetID, PublishRevision: reference.PublishRevision,
		ContentDigest: reference.ContentDigest, ApprovedLocalEndpointID: reference.EndpointID, EndpointRevision: reference.EndpointRevision,
		ProviderHealthRevision: reference.ProviderHealthRevision, CapacityRevision: reference.CapacityRevision, ProviderRevision: reference.ProviderRevision,
		IssuedAt: reservation.IssuedAt, RenewedAt: reservation.RenewedAt, ExpiresAt: reservation.FreshnessExpiresAt,
		ReleasedAt: reservation.ReleasedAt, State: string(reservation.State), ReasonCodesJSON: reasons,
	}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "lease_id"}}, DoUpdates: clause.AssignmentColumns([]string{
		"schema", "holder_id", "strategy_plan_id", "operation_id", "resource_id", "provider_reservation_id", "provider_reservation_revision",
		"provider_id", "target_id", "publish_revision", "content_digest", "approved_local_endpoint_id", "endpoint_revision",
		"provider_health_revision", "capacity_revision", "provider_revision", "issued_at", "renewed_at", "expires_at", "released_at", "state", "reason_codes_json",
	})}).Create(&row).Error
}

func nativeOperationAssignments(value NativeFallbackOperationModel) map[string]any {
	return map[string]any{
		"revision": value.Revision, "provider_reservation_id": value.ProviderReservationID, "provider_reservation_revision": value.ProviderReservationRevision,
		"core_checkpoint_id": value.CoreCheckpointID, "core_checkpoint_digest": value.CoreCheckpointDigest, "checkpoint_release_proof": value.CheckpointReleaseProof,
		"core_checkpoint_released_at": value.CoreCheckpointReleasedAt, "artifact_revision": value.ArtifactRevision, "artifact_manifest_digest": value.ArtifactManifestDigest,
		"mutation_marked_at": value.MutationMarkedAt, "workflow_state": value.WorkflowState, "after_configuration_revision": value.AfterConfigurationRevision,
		"expected_effective_revision": value.ExpectedEffectiveRevision, "effective_revision": value.EffectiveRevision, "health_result_revision": value.HealthResultRevision,
		"health_facts_json": value.HealthFactsJSON, "manager_generation": value.ManagerGeneration, "rollback_attempt_count": value.RollbackAttemptCount,
		"recovery_classification": value.RecoveryClassification, "reason_codes_json": value.ReasonCodesJSON, "recovery_bundle_json": value.RecoveryBundleJSON,
		"updated_at": value.UpdatedAt, "prepared_at": value.PreparedAt, "applied_at": value.AppliedAt, "rolled_back_at": value.RolledBackAt,
	}
}

func safeRecoveryBundle(value []byte) bool {
	if len(value) == 0 {
		return true
	}
	var object map[string]any
	if json.Unmarshal(value, &object) != nil {
		return false
	}
	lower := strings.ToLower(string(value))
	for _, forbidden := range []string{"password", "private_key", "certificate", "raw_config", "filesystem", "dsn", "environment", "token", "cookie", "http://", "https://"} {
		if strings.Contains(lower, forbidden) {
			return false
		}
	}
	return true
}

func boundedCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	if !domain.ValidContractID(value, 64) {
		return "invalid"
	}
	return value
}

func (model NativeFallbackOperationModel) ValidateJournal() error {
	if model.Schema != NativeFallbackOperationSchemaV1 || model.Revision <= 0 || !domain.ValidContractID(model.OperationID, 128) || !domain.ValidContractID(model.WorkflowState, 32) {
		return fmt.Errorf("native fallback operation journal is invalid")
	}
	return nil
}
