//go:build !linux

package sshbroker

import (
	"errors"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func RegisterHandlers(*broker.Registry) error {
	return errors.New("production SSH broker operations require Linux")
}
