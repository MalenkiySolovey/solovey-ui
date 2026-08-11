package sshmanagement

import "sync"

var (
	sharedOnce sync.Once
	shared     *Manager
)

// Shared returns the process-wide neutral authority backed by the fixed
// production broker socket. Capability discovery remains truthful when the
// broker is absent (for example in normal CI and Docker profiles).
func Shared() *Manager {
	sharedOnce.Do(func() { shared = DefaultManagerWithProvider(NewBrokerProvider(nil)) })
	return shared
}
