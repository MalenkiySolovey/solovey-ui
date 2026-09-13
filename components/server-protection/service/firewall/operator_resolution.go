package firewall

import (
	"context"
	"slices"

	operations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	repository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

type OperatorStatus struct {
	RollbackAvailable bool
	DecisionRequired  bool
	Reason            string
}

// OperatorStatus describes a possible action. The public mutation route still
// authenticates the operator and validates confirmation, policy and fresh state.
func (w Workflow) OperatorStatus(ctx context.Context, operation repository.OperationLockModel) OperatorStatus {
	if operation.Kind != operations.KindFirewall {
		return OperatorStatus{}
	}
	if operation.State == operations.StateApplied {
		checkpoint, err := w.loadCheckpointForRollback(operation.OperationID)
		return OperatorStatus{RollbackAvailable: err == nil && checkpoint.PlanRevision == operation.PlanRevision}
	}
	if operation.State != operations.StateReconcileRequired {
		return OperatorStatus{}
	}
	status := OperatorStatus{DecisionRequired: true, Reason: "firewall_recovery_needs_review"}
	snapshot, err := w.Contributions.FirewallAuthority(ctx)
	if err == nil && snapshot.HasComposition && snapshot.Composition.AppliedOperationID == operation.OperationID &&
		snapshot.Composition.Runtime.State == "RESTORE_FAILED" && snapshot.Composition.Runtime.Reason == "boot_restore_deadline_expired" {
		status.Reason = "firewall_restore_expired"
	}
	status.RollbackAvailable = w.CanResolveByRollback(ctx, operation)
	return status
}

// CanResolveByRollback projects semantic availability, never authentication or
// permission. Rollback repeats this predicate and then acquires the operation
// fence before retiring the original contribution through its ordinary path.
func (w Workflow) CanResolveByRollback(ctx context.Context, operation repository.OperationLockModel) bool {
	if operation.State != operations.StateReconcileRequired || operation.Kind != operations.KindFirewall {
		return false
	}
	return w.retainedRollbackAdmission(ctx, operation) == nil
}

// A rolling-back row records an operator decision already admitted before the
// crash. Classification permits recovery inspection only; finishRollback still
// proves current authority, absence and management health before continuation.
func (w Workflow) retainedRollbackPending(ctx context.Context, operation repository.OperationLockModel) bool {
	if operation.Kind != operations.KindFirewall || operation.State != operations.StateRollingBack {
		return false
	}
	snapshot, err := w.Contributions.FirewallAuthority(ctx)
	if err != nil {
		return false
	}
	if snapshot.HasComposition && snapshot.Composition.AppliedOperationID == operation.OperationID && retainedRuntimeFailure(snapshot.Composition.Runtime) {
		return true
	}
	transition, err := w.Contributions.FirewallTransition(ctx, operation.OperationID)
	return err == nil && !snapshot.HasComposition && len(snapshot.Contributions) == 0 &&
		transition.State == "ROLLED_BACK" && transition.RuntimeRetirementReason == "boot_restore_deadline_expired"
}

func (w Workflow) retainedRollbackAdmission(ctx context.Context, operation repository.OperationLockModel) error {
	if operation.Kind != operations.KindFirewall || operation.ResourceID != "managed-table:inet:solovey_protection" ||
		(operation.State != operations.StateReconcileRequired && operation.State != operations.StateRollingBack) {
		return operations.ErrConflict
	}
	items, err := w.Manager.List(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.OperationID != operation.OperationID && slices.Contains(repository.NonTerminalOperationLockStates(), item.State) {
			return operations.ErrConflict
		}
	}
	snapshot, err := w.Contributions.FirewallAuthority(ctx)
	if err != nil || !snapshot.HasComposition || snapshot.Composition.State != "ACTIVE" || snapshot.Composition.AppliedOperationID != operation.OperationID || len(snapshot.Contributions) != 1 {
		return operations.ErrConflict
	}
	binding := snapshot.Composition.Runtime
	if binding.State != "RESTORE_FAILED" || binding.Reason != "boot_restore_deadline_expired" {
		return operations.ErrConflict
	}
	binding.State, binding.Reason = "RESTORE_REQUIRED", "boot_runtime_loss_detected"
	if !zeroMutationExpiredRuntime(binding, w.now()) {
		return operations.ErrConflict
	}
	transition, err := w.Contributions.FirewallTransition(ctx, operation.OperationID)
	contribution := snapshot.Contributions[0]
	if err != nil || transition.Schema != FirewallTransitionSchemaV2 || transition.State != "HEALTH_VERIFIED" || transition.PreviousPresent || transition.BeforeCompositionRevision != "" ||
		contribution.AppliedOperationID != operation.OperationID || contribution.ContributionID != transition.ContributionID || contribution.SemanticRevision != transition.DesiredSemanticRevision ||
		transition.AfterCompositionRevision != snapshot.Composition.Revision || transition.ManagedPlanRevision != operation.PlanRevision || transition.ManagedPlanRevision != snapshot.Composition.ManagedPlanRevision ||
		transition.CandidateSHA256 != snapshot.Composition.CandidateSHA256 || transition.CandidateSemanticSHA256 != snapshot.Composition.CandidateSemanticSHA256 || transition.CandidateTimedMembershipSHA256 != snapshot.Composition.CandidateTimedMembershipSHA256 {
		return operations.ErrConflict
	}
	values, err := contributionsFromModels(snapshot.Contributions)
	composition, composeErr := composeFirewall(values)
	if err != nil || composeErr != nil || !matchesCommittedComposition(composition, snapshot.Composition) {
		return operations.ErrConflict
	}
	observation, err := w.ReconcileAuthority(ctx)
	if err != nil || observation.State != FirewallLiveAbsent || observation.CommittedComposition != snapshot.Composition.Revision {
		return operations.ErrConflict
	}
	return nil
}
