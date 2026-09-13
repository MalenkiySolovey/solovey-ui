//go:build !linux

package sshbroker

// NewRecoveryObserver fails closed away from Linux while preserving the same
// narrow typed capability boundary for callers and tests.
func NewRecoveryObserver(ResolvedSSHComposition) RecoveryObserver {
	return unavailableRecoveryObserver{platformKnown: true, linux: false, reason: "linux_required"}
}
