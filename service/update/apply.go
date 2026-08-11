package update

import "errors"

// ErrBrokerRequired is returned by every legacy in-process activation entry
// point. Executable replacement, rollback markers and service/process control
// belong exclusively to the typed privileged update broker.
var ErrBrokerRequired = errors.New("signed core update lifecycle and privileged broker are required")

type PipelineDeps struct {
	Client   HTTPDoer
	ExecPath string
}

func DefaultPipelineDeps() PipelineDeps { return PipelineDeps{} }

// ApplyPipeline is retained as a compatibility symbol for older optional
// component builds. It is deliberately non-mutating and fail-closed.
func ApplyPipeline(ReleaseTarget, PipelineDeps, func(UpdateStage)) error { return ErrBrokerRequired }

// RestoreBackup is not an executable rollback primitive anymore. Rollback is
// a revision-fenced broker verb over a prepared coherent release set.
func RestoreBackup(string) error { return ErrBrokerRequired }

// Legacy pending markers are ignored. Durable reconciliation uses
// update_operations_v1 and update_journal_v1.
func CheckPending(string) bool { return false }
func ClearPending(string)      {}
