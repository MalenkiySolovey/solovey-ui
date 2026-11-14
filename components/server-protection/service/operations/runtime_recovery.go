package operations

import (
	"context"
	"errors"
	"fmt"

	repository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/durable"
)

// RuntimeRecoveryError is an execution failure of a constructed recovery
// owner. It must not be confused with invalid component construction. Only
// semantic recovery owners construct it, with a fixed lifecycle step.
type RuntimeRecoveryError struct {
	Step  string
	Cause error
}

func (e *RuntimeRecoveryError) Error() string {
	step, role, primary, extended := e.Step, "runtime_recovery", 0, 0
	var persistence repository.PersistenceDiagnosticProvider
	if errors.As(e.Cause, &persistence) {
		diagnostic := persistence.PersistenceDiagnostic()
		if diagnostic.Step != "" {
			step = diagnostic.Step
		}
		if diagnostic.Role != "" {
			role = diagnostic.Role
		}
		primary, extended = diagnostic.Primary, diagnostic.Extended
	}
	return fmt.Sprintf("server-protection recovery step=%s role=%s sqlite_primary=%d sqlite_extended=%d", step, role, primary, extended)
}

func (e *RuntimeRecoveryError) Unwrap() error { return e.Cause }

func (m *Manager) reportRuntimeRecovery(err error) bool {
	var failure *RuntimeRecoveryError
	if !errors.As(err, &failure) {
		return false
	}
	if m.opts.ReportRecovery != nil {
		m.opts.ReportRecovery(failure)
	}
	return true
}

// Record only a semantic runtime owner's execution failure, while its process
// gate and latest durable operation fence are still owned here.
func (m *Manager) recordRuntimeExecutionFailure(ctx context.Context, claimed repository.OperationLockModel, err error) error {
	var runtimeErr *RuntimeRecoveryError
	if !errors.As(err, &runtimeErr) {
		return err
	}
	m.mu.Lock()
	if m.active != nil && m.active.OperationID == claimed.OperationID {
		claimed = *m.active
	}
	m.runtimeFailures[claimed.OperationID]++
	attempts := max(m.runtimeFailures[claimed.OperationID], claimed.RecoveryAttempts+1)
	m.mu.Unlock()
	if recordErr := m.store.RecordRuntimeRecoveryFailure(ctx, claimed, attempts, m.opts.Now().Unix()); recordErr != nil {
		return errors.Join(err, &RuntimeRecoveryError{Step: "runtime_failure_record", Cause: recordErr})
	}
	return err
}
