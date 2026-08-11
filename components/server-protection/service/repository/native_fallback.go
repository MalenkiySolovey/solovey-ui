package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"gorm.io/gorm"
)

func (r *Repository) NativeFallbackState(ctx context.Context, resourceID string) (domain.NativeFallbackStateV1, error) {
	if r == nil || r.db == nil {
		return domain.NativeFallbackStateV1{}, errors.New("server-protection repository is not initialized")
	}
	if !domain.ValidContractID(resourceID, 256) {
		return domain.NativeFallbackStateV1{}, errors.New("native fallback resource identity is invalid")
	}
	var row NativeFallbackStateModel
	err := r.db.WithContext(ctx).Where("resource_id = ?", resourceID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.NativeFallbackStateV1{
			Schema: domain.NativeFallbackStateSchemaV1, ResourceID: resourceID, DesiredState: domain.NativeFallbackDesired,
			SelectedVariant: domain.NativeFallbackVariantNone, ActualState: domain.NativeActualNotApplied,
			ReasonCodes: []domain.NativeFallbackReasonCode{domain.NativeReasonStateAbsent},
		}, nil
	}
	if err != nil {
		return domain.NativeFallbackStateV1{}, err
	}
	state := projectNativeFallbackState(row, false)
	if domain.NativeFallbackStateNeedsReconciliation(state.ActualState) {
		var operation NativeFallbackOperationModel
		err := r.db.WithContext(ctx).Where("operation_id = ?", row.OperationID).First(&operation).Error
		if err != nil || operation.Revision != parseNativeOperationRevision(row.OperationRevision) || operation.ResourceID != row.ResourceID ||
			!nativeWorkflowMatchesActual(operation.WorkflowState, domain.NativeFallbackActualState(row.ActualState)) {
			state.ActualState = domain.NativeActualReconcileRequired
			state.ReasonCodes = domain.CanonicalNativeFallbackReasons(state.ReasonCodes, []domain.NativeFallbackReasonCode{domain.NativeReasonStateReconciliationRequired})
		}
	}
	return state, nil
}

func ProjectNativeFallbackState(row NativeFallbackStateModel) domain.NativeFallbackStateV1 {
	return projectNativeFallbackState(row, true)
}

func projectNativeFallbackState(row NativeFallbackStateModel, distrustActive bool) domain.NativeFallbackStateV1 {
	reasons := []domain.NativeFallbackReasonCode{}
	reasonsInvalid := json.Unmarshal(row.ReasonCodesJSON, &reasons) != nil
	if reasonsInvalid {
		reasons = []domain.NativeFallbackReasonCode{domain.NativeReasonStateRecordInvalid}
	}
	state := domain.NativeFallbackStateV1{
		Schema: row.Schema, ResourceID: row.ResourceID, InboundDatabaseID: row.InboundDatabaseID,
		LatestPlanID: row.LatestPlanID, LatestPlanDigest: row.LatestPlanDigest, RuntimeIdentityRevision: row.RuntimeIdentityRevision,
		CapabilityResolverRevision: row.CapabilityResolverRevision, BeforeConfigurationRevision: row.BeforeConfigurationRevision,
		AfterConfigurationRevision: row.AfterConfigurationRevision, EffectiveRevision: row.EffectiveRevision, TargetRevision: row.TargetRevision,
		ProviderRevision: row.ProviderRevision, EndpointRevision: row.EndpointRevision, PublishRevision: row.PublishRevision,
		HealthRevision: row.HealthRevision, CapacityRevision: row.CapacityRevision, ProviderReservationID: row.ProviderReservationID,
		ProviderReservationRevision: row.ProviderReservationRevision, OperationID: row.OperationID, OperationRevision: row.OperationRevision,
		DesiredState: domain.NativeFallbackDesiredState(row.DesiredState), SelectedVariant: domain.NativeFallbackVariant(row.SelectedVariant),
		ActualState: domain.NativeFallbackActualState(row.ActualState), LastGoodCheckpointID: row.LastGoodCheckpointID,
		LastGoodCheckpointDigest: row.LastGoodCheckpointDigest, ReasonCodes: domain.CanonicalNativeFallbackReasons(reasons),
		CreatedAt: time.Unix(row.CreatedAt, 0).UTC(), UpdatedAt: time.Unix(row.UpdatedAt, 0).UTC(),
	}
	if reasonsInvalid || state.ValidateStored() != nil {
		state.Schema = domain.NativeFallbackStateSchemaV1
		state.DesiredState = domain.NativeFallbackDesired
		state.SelectedVariant = domain.NativeFallbackVariantNone
		state.ActualState = domain.NativeActualReconcileRequired
		state.ReasonCodes = domain.CanonicalNativeFallbackReasons(state.ReasonCodes, []domain.NativeFallbackReasonCode{domain.NativeReasonStateRecordInvalid})
		return state
	}
	if distrustActive && domain.NativeFallbackStateNeedsReconciliation(state.ActualState) {
		state.ActualState = domain.NativeActualReconcileRequired
		state.ReasonCodes = domain.CanonicalNativeFallbackReasons(state.ReasonCodes, []domain.NativeFallbackReasonCode{domain.NativeReasonStateReconciliationRequired})
	}
	return state
}

func parseNativeOperationRevision(value string) int {
	revision := 0
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0
		}
		revision = revision*10 + int(character-'0')
	}
	return revision
}

func nativeWorkflowMatchesActual(workflow string, actual domain.NativeFallbackActualState) bool {
	switch actual {
	case domain.NativeActualPrepared:
		return workflow == NativeWorkflowPrepared
	case domain.NativeActualApplying:
		return workflow == NativeWorkflowApplying
	case domain.NativeActualHealth:
		return workflow == NativeWorkflowHealth
	case domain.NativeActualApplied:
		return workflow == NativeWorkflowApplied
	case domain.NativeActualRollingBack:
		return workflow == NativeWorkflowRollingBack
	case domain.NativeActualRollbackFailed:
		return workflow == NativeWorkflowRollbackFailed
	case domain.NativeActualReconcileRequired:
		return workflow == NativeWorkflowReconcileRequired
	default:
		return true
	}
}

// ReconcileRestoredNativeFallbackStates removes any restored claim of live
// truth. It is intentionally not a workflow writer and cannot create rows.
func ReconcileRestoredNativeFallbackStates(ctx context.Context, db *gorm.DB, now time.Time) error {
	if db == nil || !db.Migrator().HasTable(&NativeFallbackStateModel{}) {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []NativeFallbackStateModel
		if err := tx.Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			projected := ProjectNativeFallbackState(row)
			if projected.ActualState != domain.NativeActualReconcileRequired {
				continue
			}
			reasons, err := json.Marshal(domain.CanonicalNativeFallbackReasons(projected.ReasonCodes, []domain.NativeFallbackReasonCode{domain.NativeReasonStateReconciliationRequired}))
			if err != nil {
				return err
			}
			if row.ActualState == string(domain.NativeActualReconcileRequired) && bytes.Equal(row.ReasonCodesJSON, reasons) {
				continue
			}
			if err := tx.Model(&NativeFallbackStateModel{}).Where("resource_id = ?", row.ResourceID).Updates(map[string]any{
				"actual_state": domain.NativeActualReconcileRequired, "reason_codes_json": reasons, "updated_at": now.UTC().Unix(),
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ReconcileRestoredNativeFallbackRecords invalidates historical operation
// truth as well as projected state. Provider authority remains untouched and
// startup reconciliation must re-observe the exact core/provider facts.
func ReconcileRestoredNativeFallbackRecords(ctx context.Context, db *gorm.DB, now time.Time) error {
	if err := ReconcileRestoredNativeFallbackStates(ctx, db, now); err != nil {
		return err
	}
	if db == nil || !db.Migrator().HasTable(&NativeFallbackOperationModel{}) {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reasons, _ := json.Marshal([]domain.NativeFallbackReasonCode{domain.NativeReasonStateReconciliationRequired})
	bundle, _ := json.Marshal(map[string]any{
		"failedStage": "restore", "reasonCodes": []string{"restored_state_untrusted"}, "permittedNextAction": "reconcile",
	})
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		guarding := []string{NativeWorkflowPreparing, NativeWorkflowPrepared, NativeWorkflowApplying, NativeWorkflowHealth, NativeWorkflowApplied, NativeWorkflowRollingBack, NativeWorkflowRollbackFailed, NativeWorkflowReconcileRequired}
		var operations []NativeFallbackOperationModel
		if err := tx.Where("workflow_state IN ?", guarding).Find(&operations).Error; err != nil {
			return err
		}
		for _, operation := range operations {
			if operation.WorkflowState != NativeWorkflowReconcileRequired || operation.RecoveryClassification != NativeRecoveryRestoredUntrusted || !bytes.Equal(operation.ReasonCodesJSON, reasons) {
				if err := tx.Model(&NativeFallbackOperationModel{}).Where("operation_id = ?", operation.OperationID).Updates(map[string]any{
					"workflow_state": NativeWorkflowReconcileRequired, "revision": operation.Revision + 1,
					"recovery_classification": NativeRecoveryRestoredUntrusted, "reason_codes_json": reasons,
					"recovery_bundle_json": bundle, "updated_at": now.UTC().Unix(),
				}).Error; err != nil {
					return err
				}
			}
			var state NativeFallbackStateModel
			stateErr := tx.Where("resource_id = ?", operation.ResourceID).First(&state).Error
			if stateErr != nil && !errors.Is(stateErr, gorm.ErrRecordNotFound) {
				return stateErr
			}
			if stateErr == nil && (state.ActualState != string(domain.NativeActualReconcileRequired) || !bytes.Equal(state.ReasonCodesJSON, reasons)) {
				if err := tx.Model(&NativeFallbackStateModel{}).Where("resource_id = ?", operation.ResourceID).Updates(map[string]any{
					"actual_state": domain.NativeActualReconcileRequired, "reason_codes_json": reasons, "updated_at": now.UTC().Unix(),
				}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
