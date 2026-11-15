package firewall

import (
	"context"
	"errors"
	"testing"
)

func TestExpiredReconcileRuntimeTerminalizesOnce(t *testing.T) {
	assertCurrentSSHProductionLifecycle(t, true, "", "sqlite_expired_reconcile")
}

// A deployment-generation recheck is necessary for forward mutation, but not
// for terminalizing an already expired, absent zero-mutation durable attempt.
func TestExpiredRuntimeDoesNotDependOnForwardGeneration(t *testing.T) {
	assertCurrentSSHProductionLifecycle(t, true, "", "sqlite_expired_reconcile", "generation_unavailable")
}

func TestExpiredRuntimeAndOperatorRollbackCrashWindows(t *testing.T) {
	for _, window := range []string{"before_terminal", "after_terminal", "before_retirement", "after_retirement"} {
		t.Run(window, func(t *testing.T) {
			assertCurrentSSHProductionLifecycle(t, true, "", "sqlite_expired_reconcile", window)
		})
	}
}

func TestFailedRuntimeCleanupDoesNotChurnOperation(t *testing.T) {
	assertCurrentSSHProductionLifecycle(t, true, "", "sqlite_cleanup_failed")
}

type unavailableRuntimeGeneration struct{}

func (unavailableRuntimeGeneration) Generation(context.Context) (string, error) {
	return "", errors.New("installed generation recheck unavailable")
}

func (unavailableRuntimeGeneration) RequiresBaseFirewall() bool { return true }
