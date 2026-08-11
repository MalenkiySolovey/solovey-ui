package nativefallback

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
)

// Rollback is the manual, revision-fenced entry point for the existing
// one-attempt rollback engine. It reclaims the terminal operation lock before
// delegating to that engine and adds no force or provider-release path.
func (workflow *Workflow) Rollback(ctx context.Context, request RollbackWorkflowRequestV1) (WorkflowResultV1, error) {
	if err := workflow.ready(); err != nil {
		return WorkflowResultV1{}, err
	}
	if strings.TrimSpace(request.Actor) == "" || !request.Confirmed || request.OperationRevision <= 0 ||
		!domain.ValidContractID(request.OperationID, 128) || !domain.ValidSHA256(request.PlanDigest) ||
		!domain.ValidContractID(request.ProviderReservationRevision, 128) ||
		request.IdempotencyKey != "" && !domain.ValidContractID(request.IdempotencyKey, 128) {
		return WorkflowResultV1{}, &WorkflowError{Code: "rollback_request_invalid"}
	}
	operation, err := workflow.Journal.NativeFallbackOperation(ctx, request.OperationID)
	if err != nil {
		return WorkflowResultV1{}, err
	}
	lock, lockErr := workflow.Journal.OperationByID(ctx, operation.OperationID)
	if request.IdempotencyKey != "" {
		receiptLock, receiptErr := workflow.Journal.OperationByHelperRevisionPrefix(ctx, nativeActionBindingPrefix("native-rollback", request.IdempotencyKey))
		if receiptErr != nil && !errors.Is(receiptErr, repository.ErrRecordNotFound) {
			return WorkflowResultV1{}, &WorkflowError{Code: "rollback_idempotency_unavailable"}
		}
		if receiptErr == nil && receiptLock.OperationID != request.OperationID {
			return WorkflowResultV1{}, &WorkflowError{Code: "rollback_idempotency_conflict"}
		}
		sameKey, exact := nativeActionReplay(receiptLock.HelperRevision, "native-rollback", request.IdempotencyKey, request.OperationID, request.OperationRevision, request.PlanDigest, request.ProviderReservationRevision)
		if sameKey && !exact {
			return WorkflowResultV1{}, &WorkflowError{Code: "rollback_idempotency_conflict"}
		}
		if exact && operation.WorkflowState == repository.NativeWorkflowRolledBack && receiptLock.State == protectionoperations.StateRolledBack {
			state, stateErr := workflow.Journal.NativeFallbackState(ctx, operation.ResourceID)
			return WorkflowResultV1{Operation: operation, State: state}, stateErr
		}
	}
	if operation.PlanDigest != request.PlanDigest || operation.ProviderReservationRevision != request.ProviderReservationRevision {
		return WorkflowResultV1{}, &WorkflowError{Code: "rollback_operation_stale"}
	}
	if operation.WorkflowState == repository.NativeWorkflowRolledBack {
		state, stateErr := workflow.Journal.NativeFallbackState(ctx, operation.ResourceID)
		return WorkflowResultV1{Operation: operation, State: state}, stateErr
	}
	if operation.Revision != request.OperationRevision || operation.WorkflowState != repository.NativeWorkflowApplied || operation.RollbackAttemptCount != 0 {
		return WorkflowResultV1{}, &WorkflowError{Code: "rollback_operation_stale"}
	}
	plan, err := decodeOperationPlan(operation)
	if err != nil {
		return WorkflowResultV1{}, err
	}
	if lockErr != nil || lock.Kind != protectionoperations.KindNativeFallback || lock.State != protectionoperations.StateApplied {
		return WorkflowResultV1{}, &WorkflowError{Code: "rollback_fence_invalid"}
	}
	var rollingLock repository.OperationLockModel
	if request.IdempotencyKey == "" {
		rollingLock, err = workflow.Operations.BeginRollback(ctx, lock.OperationID, lock.Revision)
	} else {
		binding := nativeActionBinding("native-rollback", request.IdempotencyKey, request.OperationID, request.OperationRevision, request.PlanDigest, request.ProviderReservationRevision)
		rollingLock, err = workflow.Operations.BeginRollbackWithBinding(ctx, lock.OperationID, lock.Revision, binding)
	}
	if err != nil {
		return WorkflowResultV1{}, err
	}
	rollingOperation, err := workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
		OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalRollingBack,
		RecoveryClassification: "operator_requested", ReasonCodes: []domain.NativeFallbackReasonCode{"operator_requested"}, Now: workflow.now(),
	})
	if err != nil {
		return WorkflowResultV1{Operation: operation}, err
	}
	provider, ok := workflow.Providers.ProviderV2(plan.Target.Reference.ProviderID)
	if !ok {
		return workflow.persistRecoveryFailure(context.WithoutCancel(ctx), rollingLock, rollingOperation, repository.NativeJournalReconcileRequired, "provider_absent", coreinboundcontrol.CheckpointStatusV1{}, nil)
	}
	reservation, err := getProviderReservation(ctx, provider, rollingOperation.ProviderReservationID)
	if err != nil || reservation.ReservationRevision != request.ProviderReservationRevision {
		return workflow.persistRecoveryFailure(context.WithoutCancel(ctx), rollingLock, rollingOperation, repository.NativeJournalReconcileRequired, "provider_state_unavailable", coreinboundcontrol.CheckpointStatusV1{}, nil)
	}
	result, rollbackErr := workflow.rollback(ctx, rollingLock, rollingOperation, plan, provider, reservation, "operator_requested", false)
	if result.Operation.WorkflowState == repository.NativeWorkflowRolledBack {
		return result, nil
	}
	return result, rollbackErr
}

func (workflow *Workflow) rollback(ctx context.Context, lock repository.OperationLockModel, operation repository.NativeFallbackOperationModel, plan domain.NativeFallbackPlanV1, provider neutralfallback.ProviderV2, observedReservation neutralfallback.ProviderTargetReservationV1, reason string, recovering bool) (WorkflowResultV1, error) {
	currentReservation, err := getProviderReservation(ctx, provider, observedReservation.ReservationID)
	if err != nil || currentReservation.HolderID != operation.OperationID || currentReservation.ExactTargetReference != plan.Target.Reference {
		return workflow.rollbackFailure(ctx, lock, operation, repository.NativeJournalReconcileRequired, "provider_state_unavailable", coreinboundcontrol.CheckpointStatusV1{}, &observedReservation, recovering)
	}
	if operation.WorkflowState != repository.NativeWorkflowRollingBack {
		if !recovering {
			lock, err = workflow.Operations.Transition(ctx, lock.OperationID, lock.Revision, protectionoperations.StateRollingBack)
			if err != nil {
				return WorkflowResultV1{}, err
			}
		}
		updatedOperation, updateErr := workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
			OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalRollingBack,
			RecoveryClassification: boundedWorkflowCode(reason), ReasonCodes: []domain.NativeFallbackReasonCode{domain.NativeFallbackReasonCode(reason)}, Now: workflow.now(),
		})
		if updateErr != nil {
			return WorkflowResultV1{Operation: operation}, updateErr
		}
		operation = updatedOperation
	} else if operation.RollbackAttemptCount != 1 {
		return workflow.rollbackFailure(ctx, lock, operation, repository.NativeJournalRollbackFailed, "automatic_rollback_already_attempted", coreinboundcontrol.CheckpointStatusV1{}, &currentReservation, recovering)
	}

	checkpoint, err := workflow.Core.InspectCheckpoint(ctx, coreinboundcontrol.InspectCheckpointRequestV1{CheckpointID: operation.CoreCheckpointID})
	if err != nil || checkpoint.IntegrityDigest != operation.CoreCheckpointDigest || checkpoint.BeforeConfigurationRevision != operation.BeforeConfigurationRevision || checkpoint.ExpectedAfterRevision != operation.ExpectedAfterRevision {
		return workflow.rollbackFailure(ctx, lock, operation, repository.NativeJournalRollbackFailed, "checkpoint_missing_or_tampered", checkpoint, &currentReservation, recovering)
	}
	proofKind := coreinboundcontrol.CheckpointProofApplyNeverCommitted
	proofDigest := operation.CheckpointReleaseProof
	currentEffective := operation.BeforeEffectiveRevision
	switch checkpoint.CurrentConfigurationRevision {
	case operation.BeforeConfigurationRevision:
		if checkpoint.State == coreinboundcontrol.CheckpointStateRestoredVerified {
			proofKind, proofDigest = coreinboundcontrol.CheckpointProofRestoreVerified, checkpoint.ProofDigest
		} else if checkpoint.State == coreinboundcontrol.CheckpointStateReleased {
			// A crash after the checkpoint release but before the final journal
			// CAS is recoverable. The exact snapshot verification below still
			// proves detachment; there is simply no checkpoint release to repeat.
		} else if checkpoint.State != coreinboundcontrol.CheckpointStatePrepared {
			if checkpoint.DetachedReleaseProof == "" {
				return workflow.rollbackFailure(ctx, lock, operation, repository.NativeJournalReconcileRequired, "checkpoint_core_state_mismatch", checkpoint, &currentReservation, recovering)
			}
			proofKind, proofDigest = coreinboundcontrol.CheckpointProofDurablyAdopted, checkpoint.DetachedReleaseProof
		}
	case operation.ExpectedAfterRevision:
		if operation.RollbackAttemptCount != 1 || checkpoint.State == coreinboundcontrol.CheckpointStateRestoredCommitted || checkpoint.State == coreinboundcontrol.CheckpointStateRestoreFailed {
			return workflow.rollbackFailure(ctx, lock, operation, repository.NativeJournalRollbackFailed, "automatic_restore_not_repeatable", checkpoint, &currentReservation, recovering)
		}
		restored, restoreErr := workflow.Core.RestoreCheckpoint(ctx, coreinboundcontrol.RestoreCheckpointRequestV1{
			CheckpointID: operation.CoreCheckpointID, ExpectedCurrentRevision: operation.ExpectedAfterRevision,
		})
		if restoreErr != nil || restored.RestoredConfigurationRevision != operation.BeforeConfigurationRevision ||
			restored.RestoredEffectiveRevision != operation.BeforeEffectiveRevision || restored.ProofDigest == "" {
			return workflow.rollbackFailure(ctx, lock, operation, repository.NativeJournalRollbackFailed, "core_restore_failed", checkpoint, &currentReservation, recovering)
		}
		proofKind, proofDigest, currentEffective = coreinboundcontrol.CheckpointProofRestoreVerified, restored.ProofDigest, restored.RestoredEffectiveRevision
	default:
		return workflow.rollbackFailure(ctx, lock, operation, repository.NativeJournalReconcileRequired, "concurrent_core_drift", checkpoint, &currentReservation, recovering)
	}

	snapshot, err := workflow.Core.Snapshot(ctx, operation.InboundDatabaseID)
	variant, variantOK := coreVariant(plan.SelectedVariant)
	if err != nil || !variantOK || snapshot.ResourceID != operation.ResourceID || snapshot.ConfigurationRevision != operation.BeforeConfigurationRevision ||
		!snapshot.Effective.RuntimeAvailable || !snapshot.Effective.Present || snapshot.Effective.Tag != plan.Resource.InboundTag || snapshot.Effective.Type != plan.Resource.InboundType ||
		snapshot.Effective.Revision != currentEffective ||
		CurrentSafeSubtreeDigest(snapshot, variant, plan.CorePreview.ReplaceDefaultToo) != plan.CorePreview.CurrentSafeSubtreeDigest {
		return workflow.rollbackFailure(ctx, lock, operation, repository.NativeJournalRollbackFailed, "previous_effective_verify_failed", checkpoint, &currentReservation, recovering)
	}
	detachedRevision := detachmentProof(operation, snapshot.ConfigurationRevision, snapshot.Effective.Revision)
	released := currentReservation
	if currentReservation.State != neutralfallback.ReservationReleased {
		released, err = releaseProvider(ctx, provider, neutralfallback.ReleaseReservationRequestV1{
			RequestID: operation.OperationID + "-release", ReservationID: currentReservation.ReservationID,
			ExpectedRevision: currentReservation.ReservationRevision, VerifiedDetachedRevision: detachedRevision,
		})
		if err != nil || released.State != neutralfallback.ReservationReleased {
			return workflow.rollbackFailure(ctx, lock, operation, repository.NativeJournalRollbackFailed, "provider_release_failed", checkpoint, &currentReservation, recovering)
		}
	}
	checkpointReleased := checkpoint.State == coreinboundcontrol.CheckpointStateReleased
	if !checkpointReleased {
		if _, err := workflow.Core.ReleaseCheckpoint(ctx, coreinboundcontrol.ReleaseCheckpointRequestV1{
			CheckpointID: operation.CoreCheckpointID, Kind: proofKind, ProofDigest: proofDigest,
		}); err != nil {
			return workflow.rollbackFailure(ctx, lock, operation, repository.NativeJournalRollbackFailed, "checkpoint_release_failed", checkpoint, &released, recovering)
		}
		checkpointReleased = true
	}
	updatedOperation, err := workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
		OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalRolledBack,
		Reservation: &released, CheckpointReleased: checkpointReleased, ReasonCodes: []domain.NativeFallbackReasonCode{domain.NativeFallbackReasonCode(reason)}, Now: workflow.now(),
	})
	if err != nil {
		return WorkflowResultV1{Operation: operation}, err
	}
	operation = updatedOperation
	if !recovering {
		if _, err := workflow.Operations.Transition(ctx, lock.OperationID, lock.Revision, protectionoperations.StateRolledBack); err != nil {
			return WorkflowResultV1{}, err
		}
	}
	state, err := workflow.Journal.NativeFallbackState(ctx, operation.ResourceID)
	return WorkflowResultV1{Operation: operation, State: state}, errors.Join(&WorkflowError{Code: reason}, err)
}

func (workflow *Workflow) persistRecoveryFailure(ctx context.Context, lock repository.OperationLockModel, operation repository.NativeFallbackOperationModel, stage repository.NativeJournalStage, reason string, checkpoint coreinboundcontrol.CheckpointStatusV1, reservation *neutralfallback.ProviderTargetReservationV1) (WorkflowResultV1, error) {
	latest, err := workflow.Journal.NativeFallbackOperation(ctx, operation.OperationID)
	if err == nil {
		operation = latest
	}
	bundle := workflow.recoveryBundle(operation, reason, checkpoint, reservation)
	bundleJSON, _ := json.Marshal(bundle)
	updatedOperation, updateErr := workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
		OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: stage,
		RecoveryClassification: boundedWorkflowCode(reason), ReasonCodes: []domain.NativeFallbackReasonCode{domain.NativeFallbackReasonCode(reason)},
		RecoveryBundleJSON: bundleJSON, Now: workflow.now(),
	})
	if updateErr != nil {
		return WorkflowResultV1{Operation: operation}, updateErr
	}
	operation = updatedOperation
	targetState := protectionoperations.StateReconcileRequired
	if stage == repository.NativeJournalRollbackFailed {
		targetState = protectionoperations.StateRollbackFailed
	}
	if lock.OperationID != "" {
		if _, transitionErr := workflow.Operations.Transition(ctx, lock.OperationID, lock.Revision, targetState); transitionErr != nil {
			return WorkflowResultV1{}, transitionErr
		}
	}
	state, stateErr := workflow.Journal.NativeFallbackState(ctx, operation.ResourceID)
	return WorkflowResultV1{Operation: operation, State: state}, errors.Join(&WorkflowError{Code: reason, Ambiguous: true}, stateErr)
}

func (workflow *Workflow) rollbackFailure(ctx context.Context, lock repository.OperationLockModel, operation repository.NativeFallbackOperationModel, stage repository.NativeJournalStage, reason string, checkpoint coreinboundcontrol.CheckpointStatusV1, reservation *neutralfallback.ProviderTargetReservationV1, recovering bool) (WorkflowResultV1, error) {
	if !recovering {
		return workflow.persistRecoveryFailure(ctx, lock, operation, stage, reason, checkpoint, reservation)
	}
	latest, err := workflow.Journal.NativeFallbackOperation(ctx, operation.OperationID)
	if err == nil {
		operation = latest
	}
	updatedOperation, err := workflow.persistRecoveryJournalFailure(ctx, operation, stage, reason, checkpoint, reservation)
	if err != nil {
		return WorkflowResultV1{Operation: operation}, err
	}
	operation = updatedOperation
	state, stateErr := workflow.Journal.NativeFallbackState(ctx, operation.ResourceID)
	return WorkflowResultV1{Operation: operation, State: state}, errors.Join(&WorkflowError{Code: reason, Ambiguous: true}, stateErr)
}

func (workflow *Workflow) persistRecoveryJournalFailure(ctx context.Context, operation repository.NativeFallbackOperationModel, stage repository.NativeJournalStage, reason string, checkpoint coreinboundcontrol.CheckpointStatusV1, reservation *neutralfallback.ProviderTargetReservationV1) (repository.NativeFallbackOperationModel, error) {
	bundleJSON, _ := json.Marshal(workflow.recoveryBundle(operation, reason, checkpoint, reservation))
	return workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
		OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: stage,
		RecoveryClassification: boundedWorkflowCode(reason), ReasonCodes: []domain.NativeFallbackReasonCode{domain.NativeFallbackReasonCode(reason)},
		RecoveryBundleJSON: bundleJSON, Now: workflow.now(),
	})
}

func (workflow *Workflow) recoveryBundle(operation repository.NativeFallbackOperationModel, reason string, checkpoint coreinboundcontrol.CheckpointStatusV1, reservation *neutralfallback.ProviderTargetReservationV1) NativeRecoveryBundleV1 {
	checkpointStatus := "unavailable"
	currentRevision := ""
	if checkpoint.CheckpointID != "" {
		checkpointStatus = strings.ToLower(string(checkpoint.State))
		currentRevision = checkpoint.CurrentConfigurationRevision
	}
	providerStatus := "unavailable"
	if reservation != nil {
		providerStatus = strings.ToLower(string(reservation.State))
	}
	return NativeRecoveryBundleV1{
		Schema: NativeRecoveryBundleSchemaV1, OperationID: operation.OperationID, ResourceID: operation.ResourceID,
		ExpectedBeforeRevision: operation.BeforeConfigurationRevision, ExpectedAfterRevision: operation.ExpectedAfterRevision,
		CurrentConfigurationRevision: currentRevision, CheckpointStatus: checkpointStatus, ProviderReservationStatus: providerStatus,
		FailedStage: boundedWorkflowCode(reason), ReasonCodes: []string{boundedWorkflowCode(reason)}, PermittedNextAction: "reconcile",
	}
}

func boundedWorkflowCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 64 {
		return "unknown"
	}
	for _, character := range value {
		if !(character == '_' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9') {
			return "unknown"
		}
	}
	return value
}
