package datalifecycle

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
)

func ProjectChange(operation model.DataLifecycleOperation) operationcoordination.Change {
	resource := "database"
	if operation.Kind == "DROP_DATA" {
		resource = "durable-owner:" + operation.OwnerID
	}
	change := operationcoordination.Change{Reference: operationcoordination.NewReference("data", resource, operation.OperationID),
		Revision: operation.Revision, ExpectedRevision: operation.ExpectedRevision, OwnerPhase: operation.State,
		CheckpointRef: operation.BackupRef, Outcome: operationcoordination.OutcomePending}
	switch operation.State {
	case "ADMITTED":
		change.Phase = "PREFLIGHT"
	case "BACKING_UP", "BACKUP_READY":
		change.Phase = "OWNER_CHECKPOINTS"
	case "RESTORING", "DROPPING":
		change.Phase = "STAGE_OR_MUTATE"
	case "VERIFYING":
		change.Phase = "VERIFY"
	case "APPLIED":
		change.Phase, change.Outcome = "COMMIT", operationcoordination.OutcomeCommitted
	case "ROLLED_BACK":
		change.Phase, change.Outcome, change.RollbackVerified = "VERIFY_ROLLBACK", operationcoordination.OutcomeRolledBack, true
	case "FAILED":
		change.Phase, change.Outcome = "FAILED", operationcoordination.OutcomeFailed
	default:
		change.Phase, change.Outcome, change.RecoveryRequired = "RECOVERY_REQUIRED", operationcoordination.OutcomeRecoveryRequired, true
	}
	if operation.RestoredUntrusted || !validPersistedDataOperation(operation) {
		change.Phase, change.Outcome, change.RecoveryRequired, change.RollbackVerified = "RECOVERY_REQUIRED", operationcoordination.OutcomeRecoveryRequired, true, false
		change.CheckpointRef, change.OwnerPhase = "", "RECOVERY_REQUIRED"
		if !validDigest(change.ExpectedRevision) {
			change.ExpectedRevision = ""
		}
		return change.WithProblem("data_lifecycle_authority_untrusted")
	}
	return change.WithProblem(operation.ReasonCode)
}

type OperationView struct {
	model.DataLifecycleOperation
	Change operationcoordination.Change `json:"change"`
}

func ProjectOperation(operation model.DataLifecycleOperation) OperationView {
	return OperationView{DataLifecycleOperation: operation, Change: ProjectChange(operation)}
}
