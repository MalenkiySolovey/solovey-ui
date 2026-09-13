package deploymentbroker

import (
	"errors"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

// Backend is the deployment-owned broker adapter selected by the trusted
// installation composition. It is not inferred from Linux, filesystem
// markers, a board, or a distribution at runtime.
type Backend string

const (
	BackendSystemdNative  Backend = "systemd-native"
	BackendPackageManaged Backend = "package-managed"
)

func ParseBackend(value string) (Backend, error) {
	backend := Backend(value)
	if err := backend.Validate(); err != nil {
		return "", err
	}
	return backend, nil
}

func (backend Backend) Validate() error {
	switch backend {
	case BackendSystemdNative, BackendPackageManaged:
		return nil
	default:
		return errors.New("deployment broker backend is not registered")
	}
}

func RegisterHandlers(registry *broker.Registry, backend Backend, checkpointAuthority broker.CompletedMutationAuthority) error {
	if registry == nil {
		return broker.StartupFailure("deployment", "deployment.lifecycle", string(backend), "registry_unavailable", errors.New("deployment broker registry is required"))
	}
	if err := backend.Validate(); err != nil {
		return broker.StartupFailure("deployment", "deployment.lifecycle", "unknown", "adapter_unregistered", err)
	}
	switch backend {
	case BackendSystemdNative:
		return registerSystemdHandlers(registry, checkpointAuthority)
	case BackendPackageManaged:
		// Package installation, upgrade, rollback, and removal remain external
		// deployment/package authority. No native mutation verbs are registered.
		return nil
	default:
		panic("validated deployment broker backend was not handled")
	}
}
