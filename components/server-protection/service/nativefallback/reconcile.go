package nativefallback

import (
	"context"
	"encoding/json"
	"errors"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
)

// Reconcile implements the operation manager's kind-specific restart backend.
// The manager has already acquired the process gate and reclaimed the exact
// persisted operation fence before this method is called.
func (workflow *Workflow) Reconcile(ctx context.Context, lock repository.OperationLockModel) (protectionoperations.ReconcileDecision, error) {
	if err := workflow.ready(); err != nil {
		return protectionoperations.ReconcileDecision{}, err
	}
	if lock.Kind != protectionoperations.KindNativeFallback {
		return protectionoperations.ReconcileDecision{}, errors.New("native reconciler received another operation kind")
	}
	operation, err := workflow.Journal.NativeFallbackOperation(ctx, lock.OperationID)
	if err != nil {
		if lock.State == protectionoperations.StatePrepared {
			page, inventoryErr := workflow.Providers.ListReservationsV2(ctx, neutralfallback.ListReservationsQueryV1{
				HolderID: lock.OperationID, Limit: neutralfallback.MaxReservationListPageV2,
			})
			if inventoryErr == nil && !page.Truncated && len(page.ReasonCodes) == 0 && len(page.Reservations) == 0 {
				return protectionoperations.ReconcileDecision{State: protectionoperations.StateCancelled, Reason: "cancelled_before_operation_journal"}, nil
			}
		}
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: "operation_record_missing"}, nil
	}
	plan, err := decodeOperationPlan(operation)
	if err != nil || operation.ResourceID != lock.ResourceID || operation.PlanDigest != lock.PlanRevision {
		updated, persistErr := workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "operation_plan_state_mismatch", coreinboundcontrol.CheckpointStatusV1{}, nil)
		if persistErr != nil {
			return protectionoperations.ReconcileDecision{}, persistErr
		}
		return decisionForNativeOperation(updated, "operation_plan_state_mismatch"), nil
	}

	reservation, provider, err := workflow.reconcileReservation(ctx, &operation, plan)
	if err != nil {
		updated, persistErr := workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "provider_absent_or_unavailable", coreinboundcontrol.CheckpointStatusV1{}, reservation)
		if persistErr != nil {
			return protectionoperations.ReconcileDecision{}, persistErr
		}
		return decisionForNativeOperation(updated, "provider_absent_or_unavailable"), nil
	}
	if operation.CoreCheckpointID == "" {
		if operation.MutationMarkedAt != nil || operation.WorkflowState != repository.NativeWorkflowPreparing {
			updated, persistErr := workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "checkpoint_missing_or_tampered", coreinboundcontrol.CheckpointStatusV1{}, reservation)
			if persistErr != nil {
				return protectionoperations.ReconcileDecision{}, persistErr
			}
			return decisionForNativeOperation(updated, "checkpoint_missing_or_tampered"), nil
		}
		checkpoint, findErr := workflow.Core.FindCheckpoint(ctx, coreinboundcontrol.FindCheckpointRequestV1{PreviewDigest: plan.CorePreview.Digest})
		if findErr != nil {
			if !coreinboundcontrol.IsAdapterError(findErr, coreinboundcontrol.ErrorCheckpointMissing) {
				updated, persistErr := workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "checkpoint_lookup_ambiguous", checkpoint, reservation)
				if persistErr != nil {
					return protectionoperations.ReconcileDecision{}, persistErr
				}
				return decisionForNativeOperation(updated, "checkpoint_lookup_ambiguous"), nil
			}
			updated, cancelErr := workflow.reconcileCancellation(ctx, operation, plan, provider, reservation, coreinboundcontrol.CheckpointStatusV1{})
			if cancelErr != nil {
				return protectionoperations.ReconcileDecision{}, cancelErr
			}
			return decisionForNativeOperation(updated, "cancelled_before_checkpoint"), nil
		}
		if reservation == nil || provider == nil || checkpoint.State != coreinboundcontrol.CheckpointStatePrepared ||
			checkpoint.PreviewDigest != plan.CorePreview.Digest || checkpoint.IntegrityDigest == "" || checkpoint.UncommittedReleaseProof == "" ||
			checkpoint.BeforeConfigurationRevision != operation.BeforeConfigurationRevision || checkpoint.ExpectedAfterRevision != operation.ExpectedAfterRevision {
			updated, persistErr := workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "checkpoint_adoption_mismatch", checkpoint, reservation)
			if persistErr != nil {
				return protectionoperations.ReconcileDecision{}, persistErr
			}
			return decisionForNativeOperation(updated, "checkpoint_adoption_mismatch"), nil
		}
		artifact, artifactErr := workflow.Journal.ArtifactByOperation(ctx, operation.OperationID)
		if errors.Is(artifactErr, repository.ErrRecordNotFound) {
			artifact, artifactErr = workflow.writePreparationArtifacts(ctx, operation, *reservation, checkpoint.CheckpointID, checkpoint.IntegrityDigest)
		}
		if artifactErr != nil || artifact.OperationID != operation.OperationID || artifact.Revision != "native-"+operation.OperationID ||
			!domain.ValidSHA256(artifact.ManifestSHA256) {
			updated, persistErr := workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "checkpoint_artifact_adoption_failed", checkpoint, reservation)
			if persistErr != nil {
				return protectionoperations.ReconcileDecision{}, persistErr
			}
			return decisionForNativeOperation(updated, "checkpoint_artifact_adoption_failed"), nil
		}
		manifest, verifyErr := workflow.Marker.VerifyRevision(artifact.Revision, artifact.ManifestSHA256)
		if verifyErr != nil || manifest.OperationID != operation.OperationID || manifest.Revision != artifact.Revision {
			updated, persistErr := workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "checkpoint_artifact_integrity_failed", checkpoint, reservation)
			if persistErr != nil {
				return protectionoperations.ReconcileDecision{}, persistErr
			}
			return decisionForNativeOperation(updated, "checkpoint_artifact_integrity_failed"), nil
		}
		operation, err = workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
			OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalPrepared,
			CheckpointID: checkpoint.CheckpointID, CheckpointDigest: checkpoint.IntegrityDigest,
			CheckpointReleaseProof: checkpoint.UncommittedReleaseProof, ArtifactRevision: artifact.Revision,
			ArtifactManifestDigest: artifact.ManifestSHA256, Now: workflow.now(),
		})
		if err != nil {
			return protectionoperations.ReconcileDecision{}, err
		}
	}

	checkpoint, inspectErr := workflow.Core.InspectCheckpoint(ctx, coreinboundcontrol.InspectCheckpointRequestV1{CheckpointID: operation.CoreCheckpointID})
	if inspectErr != nil || checkpoint.IntegrityDigest != operation.CoreCheckpointDigest || checkpoint.BeforeConfigurationRevision != operation.BeforeConfigurationRevision || checkpoint.ExpectedAfterRevision != operation.ExpectedAfterRevision {
		updated, persistErr := workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "checkpoint_missing_or_tampered", checkpoint, reservation)
		if persistErr != nil {
			return protectionoperations.ReconcileDecision{}, persistErr
		}
		return decisionForNativeOperation(updated, "checkpoint_missing_or_tampered"), nil
	}
	if restoredUntrustedNonApplied(operation) {
		updated, persistErr := workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "restored_operation_requires_reconciliation", checkpoint, reservation)
		if persistErr != nil {
			return protectionoperations.ReconcileDecision{}, persistErr
		}
		return decisionForNativeOperation(updated, "restored_operation_requires_reconciliation"), nil
	}

	switch checkpoint.CurrentConfigurationRevision {
	case operation.BeforeConfigurationRevision:
		if operation.MutationMarkedAt == nil && operation.RollbackAttemptCount == 0 {
			updated, cancelErr := workflow.reconcileCancellation(ctx, operation, plan, provider, reservation, checkpoint)
			if cancelErr != nil {
				return protectionoperations.ReconcileDecision{}, cancelErr
			}
			return decisionForNativeOperation(updated, "cancelled_before_mutation"), nil
		}
		result, rollbackErr := workflow.rollback(ctx, lock, operation, plan, provider, valueOrEmptyReservation(reservation), "restart_core_at_before", true)
		return reconcileRollbackDecision(result, rollbackErr, "restart_core_at_before")
	case operation.ExpectedAfterRevision:
		if reservation == nil || (reservation.State != neutralfallback.ReservationMutationPending && reservation.State != neutralfallback.ReservationActive) {
			updated, persistErr := workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "core_provider_drift", checkpoint, reservation)
			if persistErr != nil {
				return protectionoperations.ReconcileDecision{}, persistErr
			}
			return decisionForNativeOperation(updated, "core_provider_drift"), nil
		}
		if operation.WorkflowState == repository.NativeWorkflowRollingBack || operation.RollbackAttemptCount > 0 || checkpoint.State == coreinboundcontrol.CheckpointStateCommitted {
			result, rollbackErr := workflow.rollback(ctx, lock, operation, plan, provider, *reservation, "restart_resume_rollback", true)
			return reconcileRollbackDecision(result, rollbackErr, "restart_resume_rollback")
		}
		historicalApplied := operation.WorkflowState == repository.NativeWorkflowApplied || restoredAppliedNeedsReverification(operation)
		updated, resumeErr := workflow.resumeHealth(ctx, operation, plan, provider, *reservation, checkpoint)
		if resumeErr == nil {
			return decisionForNativeOperation(updated, "restart_health_verified"), nil
		}
		if historicalApplied {
			updated, persistErr := workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "historical_applied_reverification_failed", checkpoint, reservation)
			if persistErr != nil {
				return protectionoperations.ReconcileDecision{}, persistErr
			}
			return decisionForNativeOperation(updated, "historical_applied_reverification_failed"), nil
		}
		result, rollbackErr := workflow.rollback(ctx, lock, operation, plan, provider, *reservation, "restart_health_failed", true)
		return reconcileRollbackDecision(result, rollbackErr, "restart_health_failed")
	default:
		updated, persistErr := workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "concurrent_core_drift", checkpoint, reservation)
		if persistErr != nil {
			return protectionoperations.ReconcileDecision{}, persistErr
		}
		return decisionForNativeOperation(updated, "concurrent_core_drift"), nil
	}
}

func (workflow *Workflow) reconcileReservation(ctx context.Context, operation *repository.NativeFallbackOperationModel, plan domain.NativeFallbackPlanV1) (*neutralfallback.ProviderTargetReservationV1, neutralfallback.ProviderV2, error) {
	if operation == nil {
		return nil, nil, errors.New("operation unavailable")
	}
	if operation.ProviderReservationID == "" {
		page, err := workflow.Providers.ListReservationsV2(ctx, neutralfallback.ListReservationsQueryV1{HolderID: operation.OperationID, Limit: neutralfallback.MaxReservationListPageV2})
		if err != nil || page.Truncated || len(page.ReasonCodes) != 0 || len(page.Reservations) > 1 {
			return nil, nil, errors.New("orphan reservation inventory unavailable")
		}
		if len(page.Reservations) == 0 {
			return nil, nil, nil
		}
		orphan := page.Reservations[0]
		if orphan.HolderID != operation.OperationID || orphan.ExactTargetReference != plan.Target.Reference || orphan.State != neutralfallback.ReservationReserved {
			return &orphan, nil, errors.New("orphan reservation mismatch")
		}
		updated, err := workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
			OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalReservation, Reservation: &orphan, Now: workflow.now(),
		})
		if err != nil {
			return &orphan, nil, err
		}
		*operation = updated
	}
	provider, ok := workflow.Providers.ProviderV2(plan.Target.Reference.ProviderID)
	if !ok {
		return nil, nil, errors.New("provider absent")
	}
	reservation, err := getProviderReservation(ctx, provider, operation.ProviderReservationID)
	if err != nil || reservation.HolderID != operation.OperationID || reservation.ExactTargetReference != plan.Target.Reference {
		return nil, provider, errors.New("provider reservation unavailable")
	}
	mirror, mirrorErr := workflow.Journal.ReservationMirror(ctx, operation.OperationID)
	if mirrorErr != nil || !reservationMirrorMatchesOperation(mirror, *operation, plan, reservation) {
		return &reservation, provider, errors.New("provider reservation mirror mismatch")
	}
	if reservation.State != neutralfallback.ReservationReleased {
		status := reservation.Status(workflow.now())
		if (reservation.State != neutralfallback.ReservationReserved && reservation.State != neutralfallback.ReservationMutationPending && reservation.State != neutralfallback.ReservationActive) ||
			!status.Fresh || status.EffectiveState != reservation.State {
			return &reservation, provider, errors.New("provider reservation is not freshly authoritative")
		}
	}
	return &reservation, provider, nil
}

func (workflow *Workflow) reconcileCancellation(ctx context.Context, operation repository.NativeFallbackOperationModel, plan domain.NativeFallbackPlanV1, provider neutralfallback.ProviderV2, reservation *neutralfallback.ProviderTargetReservationV1, checkpoint coreinboundcontrol.CheckpointStatusV1) (repository.NativeFallbackOperationModel, error) {
	snapshot, err := workflow.Core.Snapshot(ctx, operation.InboundDatabaseID)
	variant, variantOK := coreVariant(plan.SelectedVariant)
	if err != nil || !variantOK || snapshot.ConfigurationRevision != operation.BeforeConfigurationRevision || snapshot.Effective.Revision != operation.BeforeEffectiveRevision ||
		CurrentSafeSubtreeDigest(snapshot, variant, plan.CorePreview.ReplaceDefaultToo) != plan.CorePreview.CurrentSafeSubtreeDigest {
		return workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "detachment_not_proven", checkpoint, reservation)
	}
	released := reservation
	if reservation != nil && reservation.State != neutralfallback.ReservationReleased {
		if provider == nil || reservation.State != neutralfallback.ReservationReserved || !reservation.Status(workflow.now()).Fresh {
			return workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "reserved_authority_not_fresh", checkpoint, reservation)
		}
		value, releaseErr := releaseProvider(ctx, provider, neutralfallback.ReleaseReservationRequestV1{
			RequestID: operation.OperationID + "-release", ReservationID: reservation.ReservationID, ExpectedRevision: reservation.ReservationRevision,
			VerifiedDetachedRevision: detachmentProof(operation, snapshot.ConfigurationRevision, snapshot.Effective.Revision),
		})
		if releaseErr != nil || value.State != neutralfallback.ReservationReleased {
			return workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "provider_release_failed", checkpoint, reservation)
		}
		released = &value
	}
	checkpointReleased := operation.CoreCheckpointID == "" || checkpoint.State == coreinboundcontrol.CheckpointStateReleased
	if operation.CoreCheckpointID != "" && !checkpointReleased {
		if checkpoint.State != coreinboundcontrol.CheckpointStatePrepared {
			return workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "checkpoint_core_state_mismatch", checkpoint, released)
		}
		if _, releaseErr := workflow.Core.ReleaseCheckpoint(ctx, coreinboundcontrol.ReleaseCheckpointRequestV1{
			CheckpointID: operation.CoreCheckpointID, Kind: coreinboundcontrol.CheckpointProofApplyNeverCommitted, ProofDigest: operation.CheckpointReleaseProof,
		}); releaseErr != nil {
			return workflow.persistRecoveryJournalFailure(ctx, operation, repository.NativeJournalReconcileRequired, "checkpoint_release_failed", checkpoint, released)
		}
		checkpointReleased = true
	}
	return workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
		OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalCancelled,
		Reservation: released, CheckpointReleased: checkpointReleased, ReasonCodes: []domain.NativeFallbackReasonCode{"restart_cancelled_before_mutation"}, Now: workflow.now(),
	})
}

func (workflow *Workflow) resumeHealth(ctx context.Context, operation repository.NativeFallbackOperationModel, plan domain.NativeFallbackPlanV1, provider neutralfallback.ProviderV2, reservation neutralfallback.ProviderTargetReservationV1, checkpoint coreinboundcontrol.CheckpointStatusV1) (repository.NativeFallbackOperationModel, error) {
	expectedEffective := operation.ExpectedEffectiveRevision
	if expectedEffective == "" {
		expectedEffective = checkpoint.CurrentEffectiveRevision
	}
	verification, err := workflow.Core.VerifyEffective(ctx, coreinboundcontrol.VerifyEffectiveRequestV1{
		CheckpointID: operation.CoreCheckpointID, ExpectedAfterRevision: operation.ExpectedAfterRevision, ExpectedEffectiveRevision: expectedEffective,
	})
	if err != nil || !verification.Verified || verification.ConfigurationRevision != operation.ExpectedAfterRevision {
		return operation, errors.New("restart effective verification failed")
	}
	if operation.WorkflowState == repository.NativeWorkflowApplying {
		operation, err = workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
			OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalHealth, Reservation: &reservation,
			AfterConfigurationRevision: operation.ExpectedAfterRevision, ExpectedEffectiveRevision: verification.EffectiveRevision,
			ManagerGeneration: verification.Observation.ManagerGeneration, Now: workflow.now(),
		})
		if err != nil {
			return operation, err
		}
	}
	healthFacts, healthRevision, err := workflow.verifyHealth(ctx, operation, plan, provider, reservation, verification)
	if err != nil {
		return operation, err
	}
	active := reservation
	if reservation.State == neutralfallback.ReservationMutationPending {
		active, err = activateProvider(ctx, provider, neutralfallback.ReservationMutationRequestV1{
			RequestID: operation.OperationID + "-activate", ReservationID: reservation.ReservationID, ExpectedRevision: reservation.ReservationRevision,
			FreshnessDurationSecs: uint32(neutralfallback.MaxActiveReservationFreshnessV1.Seconds()),
		})
		if err != nil || active.State != neutralfallback.ReservationActive {
			return operation, errors.New("restart activation failed")
		}
	}
	healthJSON, _ := json.Marshal(healthFacts)
	stage := repository.NativeJournalApplied
	if operation.WorkflowState == repository.NativeWorkflowApplied || restoredAppliedNeedsReverification(operation) {
		stage = repository.NativeJournalReverified
	}
	return workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
		OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: stage, Reservation: &active,
		EffectiveRevision: verification.EffectiveRevision, ManagerGeneration: verification.Observation.ManagerGeneration,
		HealthResultRevision: healthRevision, HealthFactsJSON: healthJSON, Now: workflow.now(),
	})
}

func restoredAppliedNeedsReverification(operation repository.NativeFallbackOperationModel) bool {
	return operation.WorkflowState == repository.NativeWorkflowReconcileRequired &&
		operation.RecoveryClassification == repository.NativeRecoveryRestoredUntrusted && operation.MutationMarkedAt != nil && operation.AppliedAt != nil &&
		operation.RollbackAttemptCount == 0 && operation.AfterConfigurationRevision == operation.ExpectedAfterRevision &&
		domain.ValidSHA256(operation.ExpectedAfterRevision) && domain.ValidSHA256(operation.ExpectedEffectiveRevision) &&
		domain.ValidSHA256(operation.EffectiveRevision) && domain.ValidSHA256(operation.HealthResultRevision) &&
		domain.ValidContractID(operation.ProviderReservationID, 128) && domain.ValidContractID(operation.ProviderReservationRevision, 128) &&
		domain.ValidContractID(operation.CoreCheckpointID, 128) && domain.ValidSHA256(operation.CoreCheckpointDigest)
}

func restoredUntrustedNonApplied(operation repository.NativeFallbackOperationModel) bool {
	return operation.WorkflowState == repository.NativeWorkflowReconcileRequired &&
		operation.RecoveryClassification == repository.NativeRecoveryRestoredUntrusted && !restoredAppliedNeedsReverification(operation)
}

func reservationMirrorMatchesOperation(mirror repository.FallbackTargetLeaseModel, operation repository.NativeFallbackOperationModel, plan domain.NativeFallbackPlanV1, reservation neutralfallback.ProviderTargetReservationV1) bool {
	reference := plan.Target.Reference
	if mirror.Schema != repository.NativeFallbackMirrorSchemaV1 || mirror.LeaseID != operation.ProviderReservationID ||
		mirror.HolderID != operation.OperationID || mirror.StrategyPlanID != operation.PlanID || mirror.OperationID != operation.OperationID || mirror.ResourceID != operation.ResourceID ||
		mirror.ProviderReservationID != operation.ProviderReservationID || mirror.ProviderReservationRevision != operation.ProviderReservationRevision ||
		mirror.ProviderID != reference.ProviderID || mirror.TargetID != reference.TargetID || mirror.PublishRevision != reference.PublishRevision ||
		mirror.ContentDigest != reference.ContentDigest || mirror.ApprovedLocalEndpointID != reference.EndpointID || mirror.EndpointRevision != reference.EndpointRevision ||
		mirror.ProviderHealthRevision != reference.ProviderHealthRevision || mirror.CapacityRevision != reference.CapacityRevision || mirror.ProviderRevision != reference.ProviderRevision {
		return false
	}
	if reservation.ReservationRevision == operation.ProviderReservationRevision {
		return mirror.State == string(reservation.State) && mirror.IssuedAt == reservation.IssuedAt && mirror.RenewedAt == reservation.RenewedAt &&
			mirror.ExpiresAt == reservation.FreshnessExpiresAt && mirror.ReleasedAt == reservation.ReleasedAt
	}
	switch {
	case operation.WorkflowState == repository.NativeWorkflowApplying && mirror.State == string(neutralfallback.ReservationReserved) && reservation.State == neutralfallback.ReservationMutationPending:
		return true
	case operation.WorkflowState == repository.NativeWorkflowHealth && mirror.State == string(neutralfallback.ReservationMutationPending) && reservation.State == neutralfallback.ReservationActive:
		return true
	case operation.WorkflowState == repository.NativeWorkflowRollingBack && reservation.State == neutralfallback.ReservationReleased:
		switch mirror.State {
		case string(neutralfallback.ReservationReserved), string(neutralfallback.ReservationMutationPending), string(neutralfallback.ReservationActive), string(neutralfallback.ReservationReconcileRequired):
			return true
		}
		return false
	default:
		return false
	}
}

func reconcileRollbackDecision(result WorkflowResultV1, rollbackErr error, reason string) (protectionoperations.ReconcileDecision, error) {
	switch result.Operation.WorkflowState {
	case repository.NativeWorkflowRolledBack, repository.NativeWorkflowRollbackFailed, repository.NativeWorkflowReconcileRequired:
		return decisionForNativeOperation(result.Operation, reason), nil
	default:
		if rollbackErr != nil {
			return protectionoperations.ReconcileDecision{}, rollbackErr
		}
		return protectionoperations.ReconcileDecision{}, errors.New("native fallback rollback did not reach a durable recovery state")
	}
}

func decisionForNativeOperation(operation repository.NativeFallbackOperationModel, reason string) protectionoperations.ReconcileDecision {
	state := protectionoperations.StateReconcileRequired
	switch operation.WorkflowState {
	case repository.NativeWorkflowApplied:
		state = protectionoperations.StateApplied
	case repository.NativeWorkflowRolledBack:
		state = protectionoperations.StateRolledBack
	case repository.NativeWorkflowRollbackFailed:
		state = protectionoperations.StateRollbackFailed
	case repository.NativeWorkflowCancelled:
		state = protectionoperations.StateCancelled
	case repository.NativeWorkflowReconcileRequired:
		state = protectionoperations.StateReconcileRequired
	}
	return protectionoperations.ReconcileDecision{State: state, Reason: reason}
}

func valueOrEmptyReservation(value *neutralfallback.ProviderTargetReservationV1) neutralfallback.ProviderTargetReservationV1 {
	if value == nil {
		return neutralfallback.ProviderTargetReservationV1{}
	}
	return *value
}
