package datalifecycle

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
)

func ProjectJob(operation model.DataLifecycleOperation) operationcoordination.Job {
	change := ProjectChange(operation)
	critical := change.Outcome == operationcoordination.OutcomePending && (operation.State == "RESTORING" || operation.State == "DROPPING" || operation.State == "VERIFYING")
	// Data has durable phases, not a trustworthy byte/percentage counter.
	return operationcoordination.ProjectJob(change, nil, critical)
}
