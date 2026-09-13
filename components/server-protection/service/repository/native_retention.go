package repository

import (
	"context"
	"errors"
	"fmt"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"gorm.io/gorm"
)

const nativeFallbackSharedOperationKind = "native_fallback"

var terminalPrunableNativeWorkflowStates = []string{
	NativeWorkflowRolledBack,
	NativeWorkflowCancelled,
}

type NativeFallbackHistoryPruneResult struct {
	DeletedOperations   int
	DeletedLocks        int
	DeletedReservations int
	DeletedBytes        int64
	Preserved           int
}

// PruneNativeFallbackHistory bounds only complete semantic history outside
// the full live closure. The resource singleton, nonterminal/recovery states,
// unreleased core checkpoints or provider reservations, retained artifacts,
// and nonterminal shared operation locks all fail closed and remain intact.
func (r *Repository) PruneNativeFallbackHistory(ctx context.Context, keepCount int, cutoff int64, keepBytes int64) (NativeFallbackHistoryPruneResult, error) {
	if r == nil || r.db == nil {
		return NativeFallbackHistoryPruneResult{}, errors.New("server-protection repository is not initialized")
	}
	if keepCount < 1 || cutoff <= 0 || keepBytes < 1 {
		return NativeFallbackHistoryPruneResult{}, errors.New("native fallback history retention limits are invalid")
	}
	result := NativeFallbackHistoryPruneResult{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var operations []NativeFallbackOperationModel
		if err := tx.Where("workflow_state IN ?", terminalPrunableNativeWorkflowStates).
			Order("updated_at DESC, id DESC").Find(&operations).Error; err != nil {
			return err
		}
		keptCount := 0
		var keptBytes int64
		for _, operation := range operations {
			closure, err := nativeFallbackClosureTx(tx, operation)
			if err != nil {
				return err
			}
			logicalBytes := nativeFallbackHistoryLogicalBytes(operation, closure.lock, closure.hasLock, closure.mirrors)
			fitsBytes := logicalBytes >= 0 && logicalBytes <= keepBytes-keptBytes
			if closure.live || (keptCount < keepCount && operation.UpdatedAt >= cutoff && fitsBytes) {
				result.Preserved++
				if !closure.live {
					keptCount++
					keptBytes += logicalBytes
				}
				continue
			}
			if len(closure.mirrors) > 0 {
				ids := make([]uint, 0, len(closure.mirrors))
				for _, mirror := range closure.mirrors {
					ids = append(ids, mirror.ID)
				}
				deleted := tx.Where("id IN ? AND operation_id = ? AND state = ?", ids, operation.OperationID, string(neutralfallback.ReservationReleased)).Delete(&FallbackTargetLeaseModel{})
				if deleted.Error != nil {
					return deleted.Error
				}
				if deleted.RowsAffected != int64(len(ids)) {
					return fmt.Errorf("native fallback reservation retention fence changed for %s", operation.OperationID)
				}
				result.DeletedReservations += len(ids)
			}
			if closure.hasLock {
				deleted := tx.Where("id = ? AND operation_id = ? AND revision = ? AND state = ? AND kind = ?", closure.lock.ID, closure.lock.OperationID, closure.lock.Revision, closure.lock.State, nativeFallbackSharedOperationKind).Delete(&OperationLockModel{})
				if deleted.Error != nil {
					return deleted.Error
				}
				if deleted.RowsAffected != 1 {
					return fmt.Errorf("native fallback shared-operation retention fence changed for %s", operation.OperationID)
				}
				result.DeletedLocks++
			}
			deleted := tx.Where("id = ? AND operation_id = ? AND revision = ? AND workflow_state = ?", operation.ID, operation.OperationID, operation.Revision, operation.WorkflowState).Delete(&NativeFallbackOperationModel{})
			if deleted.Error != nil {
				return deleted.Error
			}
			if deleted.RowsAffected != 1 {
				return fmt.Errorf("native fallback operation retention fence changed for %s", operation.OperationID)
			}
			result.DeletedOperations++
			result.DeletedBytes = nativeSaturatingAdd(result.DeletedBytes, logicalBytes)
		}
		return nil
	})
	return result, err
}

type nativeFallbackClosure struct {
	live    bool
	lock    OperationLockModel
	hasLock bool
	mirrors []FallbackTargetLeaseModel
}

func nativeFallbackClosureTx(tx *gorm.DB, operation NativeFallbackOperationModel) (nativeFallbackClosure, error) {
	closure := nativeFallbackClosure{}
	var stateCount int64
	if err := tx.Model(&NativeFallbackStateModel{}).Where("operation_id = ?", operation.OperationID).Count(&stateCount).Error; err != nil {
		return closure, err
	}
	if stateCount > 0 {
		closure.live = true
	}
	if operation.CoreCheckpointID != "" && operation.CoreCheckpointReleasedAt == nil {
		closure.live = true
	}
	var artifactCount int64
	if err := tx.Model(&ArtifactModel{}).Where("operation_id = ?", operation.OperationID).Count(&artifactCount).Error; err != nil {
		return closure, err
	}
	if artifactCount > 0 {
		closure.live = true
	}
	if err := tx.Where("operation_id = ?", operation.OperationID).Order("id ASC").Find(&closure.mirrors).Error; err != nil {
		return closure, err
	}
	if operation.ProviderReservationID != "" && len(closure.mirrors) == 0 {
		closure.live = true
	}
	for _, mirror := range closure.mirrors {
		if mirror.State != string(neutralfallback.ReservationReleased) {
			closure.live = true
		}
	}
	lockQuery := tx.Where("operation_id = ?", operation.OperationID).Limit(1).Find(&closure.lock)
	err := lockQuery.Error
	switch {
	case err == nil && lockQuery.RowsAffected == 1:
		closure.hasLock = true
		expectedState := "cancelled"
		if operation.WorkflowState == NativeWorkflowRolledBack {
			expectedState = "rolled_back"
		}
		if closure.lock.Kind != nativeFallbackSharedOperationKind || closure.lock.ResourceID != operation.ResourceID || closure.lock.State != expectedState {
			closure.live = true
		}
	case err == nil && lockQuery.RowsAffected == 0:
	case err != nil:
		return closure, err
	}
	return closure, nil
}

func nativeFallbackHistoryLogicalBytes(operation NativeFallbackOperationModel, lock OperationLockModel, hasLock bool, mirrors []FallbackTargetLeaseModel) int64 {
	logicalBytes := int64(768 + len(operation.Schema) + len(operation.OperationID) + len(operation.ResourceID) + len(operation.PlanID) + len(operation.PlanDigest) + len(operation.PlanJSON) + len(operation.RuntimeIdentityRevision) + len(operation.CapabilityResolverRevision) + len(operation.BeforeConfigurationRevision) + len(operation.ExpectedAfterRevision) + len(operation.AfterConfigurationRevision) + len(operation.BeforeEffectiveRevision) + len(operation.ExpectedEffectiveRevision) + len(operation.EffectiveRevision) + len(operation.TargetReferenceJSON) + len(operation.TargetRevision) + len(operation.ProviderRevision) + len(operation.EndpointRevision) + len(operation.PublishRevision) + len(operation.HealthRevision) + len(operation.CapacityRevision) + len(operation.ProviderReservationID) + len(operation.ProviderReservationRevision) + len(operation.CoreCheckpointID) + len(operation.CoreCheckpointDigest) + len(operation.CheckpointReleaseProof) + len(operation.ArtifactRevision) + len(operation.ArtifactManifestDigest) + len(operation.WorkflowState) + len(operation.HealthResultRevision) + len(operation.HealthFactsJSON) + len(operation.RecoveryClassification) + len(operation.ReasonCodesJSON) + len(operation.RecoveryBundleJSON))
	if hasLock {
		logicalBytes = nativeSaturatingAdd(logicalBytes, int64(256+len(lock.OperationID)+len(lock.Kind)+len(lock.ResourceID)+len(lock.Protocol)+len(lock.Listen)+len(lock.State)+len(lock.IdempotencyKey)+len(lock.PlanRevision)+len(lock.HelperRevision)+len(lock.LockedByInstanceID)+len(lock.Actor)+len(lock.RecoveryErrorCode)))
	}
	for _, mirror := range mirrors {
		logicalBytes = nativeSaturatingAdd(logicalBytes, int64(384+len(mirror.Schema)+len(mirror.LeaseID)+len(mirror.HolderID)+len(mirror.StrategyPlanID)+len(mirror.DecisionID)+len(mirror.ActionID)+len(mirror.OperationID)+len(mirror.ResourceID)+len(mirror.ProviderReservationID)+len(mirror.ProviderReservationRevision)+len(mirror.ProviderID)+len(mirror.TargetID)+len(mirror.PublishRevision)+len(mirror.ContentDigest)+len(mirror.ApprovedLocalEndpointID)+len(mirror.EndpointRevision)+len(mirror.ProviderHealthRevision)+len(mirror.CapacityRevision)+len(mirror.ProviderRevision)+len(mirror.State)+len(mirror.ReasonCodesJSON)))
	}
	return logicalBytes
}

func nativeSaturatingAdd(current, addition int64) int64 {
	if addition <= 0 {
		return current
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	if current > maxInt64-addition {
		return maxInt64
	}
	return current + addition
}
