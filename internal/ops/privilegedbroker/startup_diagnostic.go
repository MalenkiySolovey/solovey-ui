package privilegedbroker

import (
	"errors"
	"fmt"
)

// StartupDiagnosticError is the closed, secret-safe diagnostic emitted when
// broker construction cannot bind one semantic capability to its selected
// backend. Cause remains available to errors.Is/errors.As but is deliberately
// excluded from Error so paths, command output, environment, and payload data
// cannot leak through the process startup log.
type StartupDiagnosticError struct {
	Owner      string
	Capability string
	Adapter    string
	Reason     string
	cause      error
}

func (e *StartupDiagnosticError) Error() string {
	if e == nil {
		return "owner=broker capability=broker.startup adapter=unknown reason=startup_failed"
	}
	return fmt.Sprintf("owner=%s capability=%s adapter=%s reason=%s", e.Owner, e.Capability, e.Adapter, e.Reason)
}

func (e *StartupDiagnosticError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// StartupFailure constructs only bounded identifier facts. Invalid diagnostic
// input collapses to a fixed internal classification instead of reflecting
// arbitrary caller text into privileged startup logs.
func StartupFailure(owner, capability, adapter, reason string, cause error) error {
	if cause == nil {
		return nil
	}
	for _, value := range []string{owner, capability, adapter, reason} {
		if !safeIdentifier(value) {
			return &StartupDiagnosticError{Owner: "broker", Capability: "broker.startup", Adapter: "unknown", Reason: "startup_failed", cause: cause}
		}
	}
	return &StartupDiagnosticError{Owner: owner, Capability: capability, Adapter: adapter, Reason: reason, cause: cause}
}

// EnsureStartupFailure preserves an owner-local diagnostic and otherwise adds
// the composition-root context supplied by the caller.
func EnsureStartupFailure(err error, owner, capability, adapter, reason string) error {
	if err == nil {
		return nil
	}
	var diagnostic *StartupDiagnosticError
	if errors.As(err, &diagnostic) {
		return err
	}
	return StartupFailure(owner, capability, adapter, reason, err)
}
