package updatebroker

import (
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
	if registry == nil {
		return broker.StartupFailure("update", "update.lifecycle", string(mode), "registry_unavailable", errors.New("update broker registry is required"))
	}
	if err := mode.Validate(); err != nil {
		return broker.StartupFailure("update", "update.lifecycle", "unknown", "adapter_unregistered", err)
	}
	switch mode {
	case ModeNativeSelfManaged:
		return registerNativeHandlers(registry)
	case ModePackageManaged:
		// Package lifecycle remains owned by the installer/package manager. The
		// privileged broker must not advertise native activation verbs.
		return nil
	default:
		panic("validated update broker mode was not handled")
	}
}
