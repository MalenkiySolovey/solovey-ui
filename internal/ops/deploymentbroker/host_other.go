//go:build !linux

package deploymentbroker

import (
	"errors"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func RegisterHandlers(*broker.Registry) error {
	return errors.New("native deployment broker operations require Linux")
}
