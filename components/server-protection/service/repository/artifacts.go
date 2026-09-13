package repository

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

var protectedArtifactStates = []string{"prepared", "applying", "health", "health_failed", "rolling_back", "rollback_failed", "reconcile_required", "lock_suspect", "restoring_runtime"}

var terminalFirewallOperationStates = []string{"applied", "rolled_back", "abandoned", "force_unlocked", "forgotten", "cancelled"}

var terminalFirewallTransitionStates = []string{"APPLIED", "HEALTH_VERIFIED", "ROLLED_BACK", "CANCELLED", "RETIRED_RUNTIME_LOSS"}

func (r *Repository) SaveArtifact(ctx context.Context, item *ArtifactModel) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *Repository) ArtifactByOperation(ctx context.Context, operationID string) (ArtifactModel, error) {
	if r == nil || r.db == nil {
		return ArtifactModel{}, errors.New("server-protection repository is not initialized")
	}
	var item ArtifactModel
	err := r.db.WithContext(ctx).Where("operation_id = ?", operationID).Order("created_at DESC, id DESC").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ArtifactModel{}, ErrRecordNotFound
	}
	return item, err
}

func (r *Repository) ListArtifacts(ctx context.Context) ([]ArtifactModel, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("server-protection repository is not initialized")
	}
	var items []ArtifactModel
	err := r.db.WithContext(ctx).Order("created_at DESC, id DESC").Find(&items).Error
	return items, err
}

func (r *Repository) ProtectedArtifactOperations(ctx context.Context) (map[string]string, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("server-protection repository is not initialized")
	}
	return protectedArtifactOperations(r.db.WithContext(ctx))
}

func protectedArtifactOperations(db *gorm.DB) (map[string]string, error) {
	result := make(map[string]string)
	var locks []OperationLockModel
	if err := db.Where("state IN ? OR (kind <> ? AND state = ?)", protectedArtifactStates, "firewall", "applied").Find(&locks).Error; err != nil {
		return nil, err
	}
	for _, lock := range locks {
		result[lock.OperationID] = lock.State
	}
	var operations []PortOperationModel
	legacyStates := append(append([]string(nil), protectedArtifactStates...), "applied")
	if err := db.Where("state IN ?", legacyStates).Find(&operations).Error; err != nil {
		return nil, err
	}
	for _, operation := range operations {
		result[operation.OperationID] = operation.State
	}
	addReferences := func(model any, column string) error {
		var operationIDs []string
		if err := db.Model(model).Where(column+" <> ''").Distinct().Pluck(column, &operationIDs).Error; err != nil {
			return err
		}
		for _, operationID := range operationIDs {
			result[operationID] = "live_reference"
		}
		return nil
	}
	for _, reference := range []struct {
		model  any
		column string
	}{
		{&FirewallContributionModel{}, "applied_operation_id"},
		{&FirewallCompositionModel{}, "applied_operation_id"},
		{&FrontingStateV2Model{}, "latest_operation_id"},
		{&UDPGuardStateV1Model{}, "latest_operation_id"},
		{&LocalProxyStateV1Model{}, "latest_operation_id"},
		{&NativeFallbackStateModel{}, "operation_id"},
	} {
		if err := addReferences(reference.model, reference.column); err != nil {
			return nil, err
		}
	}
	var transitions []FirewallContributionTransitionModel
	if err := db.Where("state IN ?", []string{"PREPARED", "MUTATING", "ROLLING_BACK", "RECOVERY_REQUIRED"}).Find(&transitions).Error; err != nil {
		return nil, err
	}
	for _, transition := range transitions {
		result[transition.OperationID] = "firewall_transition_" + transition.State
	}
	return result, nil
}

type FirewallHistoryPruneResult struct {
	DeletedOperations  int
	DeletedTransitions int
	DeletedBytes       int64
	Preserved          int
}

// PruneFirewallHistory removes only terminal firewall operation/transition
// pairs outside the exact live reference closure. Artifact rows that still
// exist keep their operation history coherent until the artifact pruner has
// durably removed the corresponding fileset and metadata.
func (r *Repository) PruneFirewallHistory(ctx context.Context, keepCount int, cutoff int64, keepBytes int64) (FirewallHistoryPruneResult, error) {
	if r == nil || r.db == nil {
		return FirewallHistoryPruneResult{}, errors.New("server-protection repository is not initialized")
	}
	if keepCount < 1 || cutoff <= 0 || keepBytes < 1 {
		return FirewallHistoryPruneResult{}, errors.New("firewall history retention limits are invalid")
	}
	result := FirewallHistoryPruneResult{}
	err := r.durableTransition(ctx, "firewall_history_prune", func(tx *gorm.DB) error {
		protected, err := protectedArtifactOperations(tx)
		if err != nil {
			return err
		}
		var artifacts []ArtifactModel
		if err := tx.Order("created_at DESC, id DESC").Find(&artifacts).Error; err != nil {
			return err
		}
		retainedArtifactOperations := make(map[string]struct{})
		keptCount := 0
		var keptBytes int64
		for _, artifact := range artifacts {
			if _, live := protected[artifact.OperationID]; live {
				continue
			}
			if _, counted := retainedArtifactOperations[artifact.OperationID]; !counted {
				retainedArtifactOperations[artifact.OperationID] = struct{}{}
				keptCount++
			}
			if artifact.Bytes > 0 {
				if artifact.Bytes > keepBytes-keptBytes {
					keptBytes = keepBytes
				} else {
					keptBytes += artifact.Bytes
				}
			}
		}
		var operations []OperationLockModel
		if err := tx.Where("kind = ? AND state IN ?", "firewall", terminalFirewallOperationStates).
			Order("updated_at DESC, id DESC").Find(&operations).Error; err != nil {
			return err
		}
		for _, operation := range operations {
			var transition FirewallContributionTransitionModel
			transitionErr := tx.Where("operation_id = ?", operation.OperationID).First(&transition).Error
			if transitionErr != nil && !errors.Is(transitionErr, gorm.ErrRecordNotFound) {
				return transitionErr
			}
			logicalBytes := firewallHistoryLogicalBytes(operation, transition, transitionErr == nil)
			_, live := protected[operation.OperationID]
			_, retainedArtifact := retainedArtifactOperations[operation.OperationID]
			terminalTransition := transitionErr != nil || artifactContainsString(terminalFirewallTransitionStates, transition.State)
			fitsBytes := logicalBytes >= 0 && logicalBytes <= keepBytes-keptBytes
			if live || retainedArtifact || !terminalTransition || (keptCount < keepCount && operation.UpdatedAt >= cutoff && fitsBytes) {
				result.Preserved++
				if !live && !retainedArtifact && terminalTransition {
					keptCount++
					keptBytes += logicalBytes
				}
				continue
			}
			if transitionErr == nil {
				deleted := tx.Where("operation_id = ? AND state = ?", transition.OperationID, transition.State).Delete(&FirewallContributionTransitionModel{})
				if deleted.Error != nil {
					return deleted.Error
				}
				if deleted.RowsAffected != 1 {
					return fmt.Errorf("firewall transition retention fence changed for %s", operation.OperationID)
				}
				result.DeletedTransitions++
			}
			deleted := tx.Where("id = ? AND operation_id = ? AND revision = ? AND state = ?", operation.ID, operation.OperationID, operation.Revision, operation.State).Delete(&OperationLockModel{})
			if deleted.Error != nil {
				return deleted.Error
			}
			if deleted.RowsAffected != 1 {
				return fmt.Errorf("firewall operation retention fence changed for %s", operation.OperationID)
			}
			result.DeletedOperations++
			result.DeletedBytes += logicalBytes
		}
		return nil
	})
	return result, err
}

func firewallHistoryLogicalBytes(operation OperationLockModel, transition FirewallContributionTransitionModel, hasTransition bool) int64 {
	bytes := int64(256 + len(operation.OperationID) + len(operation.Kind) + len(operation.ResourceID) + len(operation.Protocol) + len(operation.Listen) + len(operation.State) + len(operation.IdempotencyKey) + len(operation.PlanRevision) + len(operation.HelperRevision) + len(operation.LockedByInstanceID) + len(operation.Actor) + len(operation.RecoveryErrorCode))
	if hasTransition {
		bytes += int64(512 + len(transition.OperationID) + len(transition.Schema) + len(transition.ContributionID) + len(transition.PreviousSemanticRevision) + len(transition.PreviousJSON) + len(transition.DesiredSemanticRevision) + len(transition.DesiredJSON) + len(transition.BeforeCompositionRevision) + len(transition.AfterCompositionRevision) + len(transition.ManagedPlanRevision) + len(transition.CandidateSHA256) + len(transition.CandidateSemanticSHA256) + len(transition.CandidateTimedMembershipSHA256) + len(transition.State) + len(transition.HealthProviderInstance) + len(transition.HealthObservationRevision))
	}
	return bytes
}

func artifactContainsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func (r *Repository) DeleteArtifact(ctx context.Context, id uint) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	return r.db.WithContext(ctx).Delete(&ArtifactModel{}, id).Error
}

func (r *Repository) MarkOperationRecovery(ctx context.Context, operationID string, attempts int, at int64, code string) error {
	if r == nil || r.db == nil {
		return errors.New("server-protection repository is not initialized")
	}
	return r.db.WithContext(ctx).Model(&OperationLockModel{}).Where("operation_id = ?", operationID).
		Updates(map[string]any{"recovery_attempts": attempts, "last_recovery_at": at, "recovery_error_code": code, "updated_at": at}).Error
}
