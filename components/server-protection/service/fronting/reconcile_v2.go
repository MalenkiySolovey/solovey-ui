package fronting

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

// V2Reconciler is the restart adapter for v2 records. The operations manager
// owns the process gate and freshly reclaimed persisted fence before this code
// is called. It never repeats a forward switch or a lease acquisition.
type V2Reconciler struct {
	Workflow *Workflow
}

func (r V2Reconciler) Reconcile(ctx context.Context, operation protectionrepository.OperationLockModel) (protectionoperations.ReconcileDecision, error) {
	w := r.Workflow
	if w == nil || w.readyV2() != nil || operation.Kind != protectionoperations.KindFronting {
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: "reconcile_required"}, nil
	}
	checkpoint, err := w.loadV2(operation.OperationID)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateRollbackFailed, Reason: "artifact_integrity_failed"}, nil
		}
		return r.reconcileMissingCheckpoint(ctx, operation)
	}
	if operation.State == protectionoperations.StateRollbackFailed || checkpoint.RollbackAttemptCount > 1 {
		r.bundle(ctx, operation, &checkpoint, protectionoperations.StateRollbackFailed, "rollback_failed")
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateRollbackFailed, Reason: "automatic_rollback_already_attempted"}, nil
	}
	if checkpoint.CandidateRevision != "" && w.verifyPreparedArtifactV2(checkpoint) != nil {
		r.bundle(ctx, operation, &checkpoint, protectionoperations.StateRollbackFailed, "artifact_integrity_failed")
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateRollbackFailed, Reason: "artifact_integrity_failed"}, nil
	}
	if !checkpoint.MutationMarker && len(checkpoint.EndpointLeases)+len(checkpoint.FallbackReservations) == 0 {
		return r.reconcileMissingCheckpoint(ctx, operation)
	}
	if !checkpoint.MutationMarker {
		specs, specErr := targetAuthoritySpecsV2(checkpoint.Plan)
		if specErr != nil || len(checkpoint.EndpointLeases)+len(checkpoint.FallbackReservations) != len(specs) {
			// An acquire can commit at the provider while its response is lost.
			// The incomplete checkpoint is not authority; enumerate the exact
			// operation holder so every safely reserved target is reconciled.
			return r.reconcileMissingCheckpoint(ctx, operation)
		}
	}
	authorities, leaseCode := w.currentTargetAuthoritiesV2(ctx, checkpoint)
	if leaseCode != "" {
		r.bundle(ctx, operation, &checkpoint, protectionoperations.StateReconcileRequired, leaseCode)
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: leaseCode}, nil
	}
	capabilities, capErr := w.capabilities(ctx)
	if capErr != nil || !frontingCapabilitiesAvailable(capabilities) || engineIdentityRevisionV2(capabilities) != checkpoint.EngineIdentityRevision {
		r.bundle(ctx, operation, &checkpoint, protectionoperations.StateReconcileRequired, "runtime_identity_stale")
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: "runtime_identity_stale"}, nil
	}
	activePrevious := capabilities.Nginx.ActiveRevision == checkpoint.PreviousRevision && capabilities.Nginx.ActiveSHA256 == checkpoint.PreviousSHA256
	activeCandidate := checkpoint.CandidateRevision != "" && capabilities.Nginx.ActiveRevision == checkpoint.CandidateRevision && capabilities.Nginx.ActiveSHA256 == checkpoint.CandidateSHA256
	if !checkpoint.MutationMarker {
		if !activePrevious || activeCandidate || !authoritiesReservedOrReleasedV2(authorities) {
			r.bundle(ctx, operation, &checkpoint, protectionoperations.StateReconcileRequired, "reconcile_required")
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: "reconcile_required"}, nil
		}
		if authoritiesAllReleasedV2(authorities) {
			updateCheckpointAuthoritiesV2(&checkpoint, authorities)
			_ = w.saveV2(&checkpoint, "restart_cancelled_lease_already_released")
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateCancelled, Reason: "pre_mutation_lease_already_released"}, nil
		}
		released, code := releaseTargetAuthoritiesV2(ctx, authorities, operation.OperationID+"-restart-cancel", preMutationDetachmentRevisionV2(checkpoint), w.nowV2())
		if code != "" {
			updateCheckpointAuthoritiesV2(&checkpoint, released)
			_ = w.saveV2(&checkpoint, "restart_cancel_release_partial")
			r.bundle(ctx, operation, &checkpoint, protectionoperations.StateReconcileRequired, code)
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: code}, nil
		}
		updateCheckpointAuthoritiesV2(&checkpoint, released)
		_ = w.saveV2(&checkpoint, "restart_cancelled_before_mutation")
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateCancelled, Reason: "pre_mutation_orphan_released"}, nil
	}
	if activeCandidate {
		if operation.State == protectionoperations.StateRollingBack || checkpoint.RollbackAttemptCount > 0 {
			r.bundle(ctx, operation, &checkpoint, protectionoperations.StateRollbackFailed, "rollback_failed")
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateRollbackFailed, Reason: "interrupted_rollback_candidate_still_active"}, nil
		}
		if !authoritiesPendingOrActiveV2(authorities) {
			r.bundle(ctx, operation, &checkpoint, protectionoperations.StateReconcileRequired, "lease_stale")
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: "lease_stale"}, nil
		}
		verification, code := w.verifyEngineRevisionV2(ctx, operation, checkpoint.CandidateRevision, checkpoint.CandidateSHA256, capabilities.Nginx.Binary, helperListenerV2(checkpoint.Plan.PublicSocket))
		if code != "" {
			r.bundle(ctx, operation, &checkpoint, protectionoperations.StateReconcileRequired, code)
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: code}, nil
		}
		recordEngineVerificationV2(&checkpoint, verification)
		activeCapability, activeCode := w.revalidateActiveBindingsV2(ctx, checkpoint.Plan)
		if activeCode != "" {
			r.bundle(ctx, operation, &checkpoint, protectionoperations.StateReconcileRequired, activeCode)
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: activeCode}, nil
		}
		checkpoint.ActiveStrategyCapabilityRevision = activeCapability
		updateCheckpointAuthoritiesV2(&checkpoint, authorities)
		if healthCode := w.checkStrategyHealthV2(ctx, operation, &checkpoint); healthCode != "" {
			r.bundle(ctx, operation, &checkpoint, protectionoperations.StateReconcileRequired, healthCode)
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: healthCode}, nil
		}
		active, activationCode := activateTargetAuthoritiesV2(ctx, authorities, operation.OperationID+"-restart-activate", w.nowV2())
		if activationCode != "" {
			updateCheckpointAuthoritiesV2(&checkpoint, active)
			_ = w.saveV2(&checkpoint, "restart_activation_partial")
			r.bundle(ctx, operation, &checkpoint, protectionoperations.StateReconcileRequired, activationCode)
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: activationCode}, nil
		}
		updateCheckpointAuthoritiesV2(&checkpoint, active)
		_ = w.saveV2(&checkpoint, "restart_applied_reverified")
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateApplied, Reason: "candidate_active_reverified"}, nil
	}
	if activePrevious {
		if _, code := w.verifyEngineRevisionV2(ctx, operation, checkpoint.PreviousRevision, checkpoint.PreviousSHA256, capabilities.Nginx.Binary, checkpoint.PreviousListeners); code != "" {
			r.bundle(ctx, operation, &checkpoint, protectionoperations.StateRollbackFailed, code)
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateRollbackFailed, Reason: code}, nil
		}
		if healthFailed(boundedHealth(ctx, w.RollbackHealth, nil, "fronting_recovery_health_timeout")) {
			r.bundle(ctx, operation, &checkpoint, protectionoperations.StateRollbackFailed, "rollback_failed")
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateRollbackFailed, Reason: "rollback_failed"}, nil
		}
		checkpoint.Detached, checkpoint.ActualActiveRevision = true, checkpoint.PreviousRevision
		if authoritiesAllReleasedV2(authorities) {
			updateCheckpointAuthoritiesV2(&checkpoint, authorities)
			_ = w.saveV2(&checkpoint, "restart_previous_verified_lease_already_released")
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateRolledBack, Reason: "previous_active_lease_already_released"}, nil
		}
		released, code := releaseTargetAuthoritiesV2(ctx, authorities, operation.OperationID+"-restart-release", detachmentRevisionV2(checkpoint), w.nowV2())
		if code != "" {
			updateCheckpointAuthoritiesV2(&checkpoint, released)
			_ = w.saveV2(&checkpoint, "restart_release_partial")
			r.bundle(ctx, operation, &checkpoint, protectionoperations.StateRollbackFailed, code)
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateRollbackFailed, Reason: code}, nil
		}
		updateCheckpointAuthoritiesV2(&checkpoint, released)
		_ = w.saveV2(&checkpoint, "restart_previous_verified_released")
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateRolledBack, Reason: "previous_active_detached"}, nil
	}
	r.bundle(ctx, operation, &checkpoint, protectionoperations.StateReconcileRequired, "rollback_drift")
	return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: "rollback_drift"}, nil
}

func (r V2Reconciler) reconcileMissingCheckpoint(ctx context.Context, operation protectionrepository.OperationLockModel) (protectionoperations.ReconcileDecision, error) {
	leases, err := safeLeaseListByHolderV2(ctx, r.Workflow.V2Leases, operation.OperationID)
	if err != nil || len(leases) > MaxFixedTargetsV1 {
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: "lease_conflict"}, nil
	}
	reservations := []fallbacktargets.ProviderTargetReservationV1{}
	if r.Workflow.V2Fallbacks != nil {
		listed, listErr := r.Workflow.V2Fallbacks.ListReservationsV2(ctx, fallbacktargets.ListReservationsQueryV1{HolderID: operation.OperationID, Limit: fallbacktargets.MaxReservationListPageV2})
		if listErr != nil || listed.Truncated || listed.Continuation != "" || len(listed.ReasonCodes) != 0 || len(listed.Reservations) > MaxFixedTargetsV1 {
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: "lease_conflict"}, nil
		}
		reservations = listed.Reservations
	}
	if len(leases)+len(reservations) == 0 {
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateCancelled, Reason: "operation_without_lease_or_marker"}, nil
	}
	current := make([]currentTargetAuthorityV2, 0, len(leases)+len(reservations))
	allReleased := true
	for _, lease := range leases {
		if lease.Validate() != nil || lease.HolderID != operation.OperationID || lease.State != hostresources.EndpointLeaseReserved && lease.State != hostresources.EndpointLeaseReleased {
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: "lease_stale"}, nil
		}
		provider, ok := r.Workflow.V2Leases.EndpointLeaseProviderV1(lease.AuthorityProviderID)
		if !ok || provider == nil || strings.TrimSpace(provider.ProviderID()) != lease.AuthorityProviderID {
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: "lease_lost"}, nil
		}
		allReleased = allReleased && lease.State == hostresources.EndpointLeaseReleased
		current = append(current, currentTargetAuthorityV2{Spec: targetAuthoritySpecV2{Kind: targetAuthorityEndpointV2,
			ReferenceRevision: lease.ExactReference.CanonicalReferenceRevision, Endpoint: lease.ExactReference}, EndpointProvider: provider, EndpointLease: lease})
	}
	for _, reservation := range reservations {
		if reservation.Validate() != nil || reservation.HolderID != operation.OperationID || reservation.Purpose != fallbacktargets.ReservationPurposeFronting ||
			reservation.State != fallbacktargets.ReservationReserved && reservation.State != fallbacktargets.ReservationReleased {
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: "lease_stale"}, nil
		}
		provider, ok := r.Workflow.V2Fallbacks.ProviderV2(reservation.ExactTargetReference.ProviderID)
		if !ok || provider == nil || strings.TrimSpace(provider.ProviderID()) != reservation.ExactTargetReference.ProviderID {
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: "lease_lost"}, nil
		}
		allReleased = allReleased && reservation.State == fallbacktargets.ReservationReleased
		current = append(current, currentTargetAuthorityV2{Spec: targetAuthoritySpecV2{Kind: targetAuthorityFallbackV2,
			ReferenceRevision: v2Revision(reservation.ExactTargetReference), Fallback: reservation.ExactTargetReference}, FallbackProvider: provider, FallbackReservation: reservation})
	}
	if allReleased {
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateCancelled, Reason: "orphan_lease_already_released"}, nil
	}
	sort.Slice(current, func(left, right int) bool {
		return current[left].Spec.ReferenceRevision < current[right].Spec.ReferenceRevision
	})
	released, code := releaseTargetAuthoritiesV2(ctx, current, operation.OperationID+"-orphan-release", v2Revision(struct {
		Operation string
		Marker    bool
	}{operation.OperationID, false}), r.Workflow.nowV2())
	if code != "" || !authoritiesAllReleasedV2(released) {
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: firstV2Code(code, "lease_lost")}, nil
	}
	return protectionoperations.ReconcileDecision{State: protectionoperations.StateCancelled, Reason: "orphan_reserved_lease_released"}, nil
}

func (r V2Reconciler) bundle(ctx context.Context, operation protectionrepository.OperationLockModel, checkpoint *CheckpointV2, state, code string) {
	checkpoint.FailedStage = code
	checkpoint.RecoveryClassification = state
	checkpoint.ReasonCodes = canonicalV2ReasonCodes(append(checkpoint.ReasonCodes, code))
	_ = r.Workflow.saveV2(checkpoint, state)
	if checkpoint.ArtifactRevision != "" && r.Workflow.Recovery != nil {
		bundleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		_ = r.Workflow.Recovery.CreateBundle(bundleCtx, operation, state)
	}
}

func authoritiesReservedOrReleasedV2(authorities []currentTargetAuthorityV2) bool {
	for _, authority := range authorities {
		if authority.Spec.Kind == targetAuthorityEndpointV2 && authority.EndpointLease.State != hostresources.EndpointLeaseReserved && authority.EndpointLease.State != hostresources.EndpointLeaseReleased ||
			authority.Spec.Kind == targetAuthorityFallbackV2 && authority.FallbackReservation.State != fallbacktargets.ReservationReserved && authority.FallbackReservation.State != fallbacktargets.ReservationReleased {
			return false
		}
	}
	return len(authorities) > 0
}

func authoritiesAllReleasedV2(authorities []currentTargetAuthorityV2) bool {
	return authoritiesInStateV2(authorities, hostresources.EndpointLeaseReleased, fallbacktargets.ReservationReleased)
}

func authoritiesPendingOrActiveV2(authorities []currentTargetAuthorityV2) bool {
	for _, authority := range authorities {
		if authority.Spec.Kind == targetAuthorityEndpointV2 && authority.EndpointLease.State != hostresources.EndpointLeaseMutationPending && authority.EndpointLease.State != hostresources.EndpointLeaseActive ||
			authority.Spec.Kind == targetAuthorityFallbackV2 && authority.FallbackReservation.State != fallbacktargets.ReservationMutationPending && authority.FallbackReservation.State != fallbacktargets.ReservationActive {
			return false
		}
	}
	return len(authorities) > 0
}

var _ protectionoperations.Reconciler = V2Reconciler{}
