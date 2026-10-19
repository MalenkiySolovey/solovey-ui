package firewall

import (
	"context"
	"errors"
	"strings"

	operations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	repository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	networkplan "github.com/MalenkiySolovey/solovey-ui/network/planner"
)

// ProjectOperation reuses R3 envelopes over the existing durable owner row.
// Process restart reconstructs this projection; no Planner operation store,
// journal, scheduler, cancellation or percentage is invented.
func ProjectOperation(operation repository.OperationLockModel) (operationcoordination.Change, operationcoordination.Job) {
	change := operationcoordination.Change{Reference: operationcoordination.NewReference("server-protection", operation.ResourceID, operation.OperationID),
		Revision: uint64(max(operation.Revision, 0)), ExpectedRevision: operation.PlanRevision, OwnerPhase: operation.State,
		Phase: "PLAN", Outcome: operationcoordination.OutcomePending}
	switch operation.State {
	case operations.StatePrepared:
		change.Phase = "PREFLIGHT"
	case operations.StateApplying, operations.StateRestoringRuntime:
		change.Phase = "APPLY"
	case operations.StateHealth:
		change.Phase = "VERIFY"
	case operations.StateApplied:
		change.Phase, change.Outcome = "COMMIT", operationcoordination.OutcomeCommitted
	case operations.StateRollingBack:
		change.Phase = "ROLLBACK"
	case operations.StateRolledBack:
		change.Phase, change.Outcome, change.RollbackVerified = "ROLLED_BACK", operationcoordination.OutcomeRolledBack, true
	case operations.StateReconcileRequired, operations.StateRollbackFailed, operations.StateLockSuspect:
		change.Phase, change.Outcome, change.RecoveryRequired = "RECOVERY_REQUIRED", operationcoordination.OutcomeRecoveryRequired, true
	case operations.StateHealthFailed:
		change.Phase = "VERIFY"
	case operations.StateCancelled, operations.StateAbandoned, operations.StateForgotten, operations.StateForceUnlocked:
		change.Phase, change.Outcome = "STOPPED", operationcoordination.OutcomeFailed
	default:
		change.Phase, change.Outcome, change.RecoveryRequired = "RECOVERY_REQUIRED", operationcoordination.OutcomeRecoveryRequired, true
	}
	change = change.WithProblem(operation.RecoveryErrorCode)
	critical := operation.State == operations.StateApplying || operation.State == operations.StateRollingBack || operation.State == operations.StateRestoringRuntime
	return change, operationcoordination.ProjectJob(change, nil, critical)
}

func (w Workflow) Apply(ctx context.Context, input ApplyInput) (Result, error) {
	result, err := w.apply(ctx, input)
	return w.projectNetworkResult(ctx, input.OperationID, result, err), err
}

func (w Workflow) Rollback(ctx context.Context, operationID, confirmation string) (Result, error) {
	result, err := w.rollbackRequested(ctx, operationID, confirmation)
	return w.projectNetworkResult(ctx, operationID, result, err), err
}

func (w Workflow) projectNetworkResult(ctx context.Context, operationID string, result Result, cause error) Result {
	if w.Manager == nil {
		return result
	}
	operation, err := w.operation(ctx, operationID)
	if err != nil || operation.Kind != operations.KindFirewall {
		return result
	}
	change, job := ProjectOperation(operation)
	result.Change, result.Job = &change, &job
	if result.OperationID == "" {
		result.OperationID, result.State, result.Revision = operation.OperationID, operation.State, operation.Revision
	}
	var conflict *networkplan.Conflict
	if errors.As(cause, &conflict) {
		result.ReasonCodes = uniqueSorted(append(result.ReasonCodes, "network_"+strings.ToLower(string(conflict.Reason))))
	}
	return result
}
