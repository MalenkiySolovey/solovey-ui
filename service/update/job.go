package update

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
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
