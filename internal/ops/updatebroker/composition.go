package updatebroker

import (
	"context"
	"errors"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

// Mode is the update lifecycle projected by the selected deployment. Native
// self-management and package management are mutually exclusive authorities;
// neither is discovered from the running OS by this broker.
type Mode string

const (
	ModeNativeSelfManaged Mode = "native-self-managed"
	ModePackageManaged    Mode = "package-managed"
)

func ParseMode(value string) (Mode, error) {
	mode := Mode(value)
	if err := mode.Validate(); err != nil {
		return "", err
	}
	return mode, nil
}

func (mode Mode) Validate() error {
	switch mode {
	case ModeNativeSelfManaged, ModePackageManaged:
		return nil
	default:
		return errors.New("update broker mode is not registered")
	}
}

func RegisterHandlers(registry *broker.Registry, mode Mode) error {
	return NewRuntime().RegisterHandlers(registry, mode)
}

// Runtime binds the Update handlers and their recovery callback to one owner.
// The composition root calls Reconcile from its existing bounded authority
// watcher; this object does not start another worker or own another journal.
type Runtime struct {
	registered bool
	reconcile  func(context.Context) error
}

func NewRuntime() *Runtime { return &Runtime{} }

func (runtime *Runtime) RegisterHandlers(registry *broker.Registry, mode Mode) error {
	if runtime == nil || runtime.registered {
		return broker.StartupFailure("update", "update.lifecycle", string(mode), "runtime_binding_invalid", errors.New("update runtime can be bound only once"))
	}
	if registry == nil {
		return broker.StartupFailure("update", "update.lifecycle", string(mode), "registry_unavailable", errors.New("update broker registry is required"))
	}
	if err := mode.Validate(); err != nil {
		return broker.StartupFailure("update", "update.lifecycle", "unknown", "adapter_unregistered", err)
	}
	switch mode {
	case ModeNativeSelfManaged:
		reconcile, err := registerNativeHandlers(registry)
		if err != nil {
			return err
		}
		runtime.reconcile = reconcile
	case ModePackageManaged:
		// Package lifecycle remains owned by the installer/package manager. The
		// privileged broker must not advertise native activation verbs.
	default:
		panic("validated update broker mode was not handled")
	}
	runtime.registered = true
	return nil
}

func (runtime *Runtime) Reconcile(ctx context.Context) error {
	if runtime == nil || !runtime.registered || ctx == nil {
		return errors.New("update runtime is not bound")
	}
	if ctx.Err() != nil || runtime.reconcile == nil {
		return ctx.Err()
	}
	return runtime.reconcile(ctx)
}
