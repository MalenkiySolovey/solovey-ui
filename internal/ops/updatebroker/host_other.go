//go:build !linux

package updatebroker

import (
	"context"
	"errors"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func registerNativeHandlers(*broker.Registry) (func(context.Context) error, error) {
	return nil, broker.StartupFailure("update", "update.lifecycle", "native-self-managed", "linux_required", errors.New("native update broker operations require Linux"))
}
