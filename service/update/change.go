package update

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
)

func ProjectChange(operation model.UpdateOperation) operationcoordination.Change {
	if operation.RestoredUntrusted {
		operation = restoredOperationRecoveryProjection(operation)
	} else if !validPersistedOperation(operation) {
		operation = operationRecoveryProjection(operation)
		operation.BackupRef = ""
	}
	if !validDigest(operation.DeploymentRevision) {
		operation.DeploymentRevision = ""
	}
	change := operationcoordination.Change{Reference: operationcoordination.NewReference("update", "panel", operation.OperationID),
		Revision: operation.Revision, ExpectedRevision: operation.DeploymentRevision, OwnerPhase: operation.State,
		CheckpointRef: operation.BackupRef, Outcome: operationcoordination.OutcomePending}
	switch State(operation.State) {
	case StateDownloading, StateDownloaded, StateVerifying, StateVerified:
		change.Phase = "STAGE_OR_MUTATE"
	case StatePreflighting:
		change.Phase = "PREFLIGHT"
	case StatePrepared:
		change.Phase = "OWNER_CHECKPOINTS"
	case StateActivating:
		change.Phase = "ACTIVATE"
	case StateVerifyingActive:
		change.Phase = "VERIFY"
	case StateApplied:
		change.Phase, change.Outcome = "COMMIT", operationcoordination.OutcomeCommitted
	case StateRollbackPending, StateRollingBack:
		change.Phase = "ROLLBACK"
	case StateRolledBack:
		change.Phase, change.Outcome, change.RollbackVerified = "VERIFY_ROLLBACK", operationcoordination.OutcomeRolledBack, true
	case StateFailed:
		change.Phase, change.Outcome = "FAILED", operationcoordination.OutcomeFailed
	default:
		change.Phase, change.Outcome, change.RecoveryRequired = "RECOVERY_REQUIRED", operationcoordination.OutcomeRecoveryRequired, true
	}
	return change.WithProblem(operation.ReasonCode)
}

// OperationView preserves the existing owner fields and adds a read projection.
type OperationView struct {
	model.UpdateOperation
	Change operationcoordination.Change `json:"change"`
}

func ProjectOperation(operation model.UpdateOperation) OperationView {
	return OperationView{UpdateOperation: operation, Change: ProjectChange(operation)}
}
