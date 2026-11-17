package update

import (
	"github.com/MalenkiySolovey/solovey-backend/operations/coordination"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
)

func ProjectJob(operation model.UpdateOperation) operationcoordination.Job {
	change := ProjectChange(operation)
	var progress *operationcoordination.Progress
	if !operation.RestoredUntrusted && validPersistedOperation(operation) && State(operation.State) != StateFailed && State(operation.State) != StateRecoveryRequired {
		progress = &operationcoordination.Progress{Completed: operation.BytesCompleted, Total: operation.BytesTotal, Unit: "bytes"}
	}
	critical := false
	switch State(operation.State) {
	case StateActivating, StateVerifyingActive, StateRollbackPending, StateRollingBack:
		critical = change.Outcome == operationcoordination.OutcomePending
	}
	return operationcoordination.ProjectJob(change, progress, critical)
}
