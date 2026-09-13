//go:build !linux

package sshbroker

import (
	"errors"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func RegisterHandlers(*broker.Registry, Composition, broker.CompletedMutationAuthority) error {
	return errors.New("production SSH broker operations require Linux")
}

func RegisterHandlersFromResolved(*broker.Registry, ResolvedSSHComposition, broker.CompletedMutationAuthority) error {
	return errors.New("production SSH broker operations require Linux")
}
