//go:build !linux

package deploymentbroker

import (
	"errors"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func registerSystemdHandlers(*broker.Registry, broker.CompletedMutationAuthority) error {
	return broker.StartupFailure("deployment", "service.supervision", "systemd", "linux_required", errors.New("native deployment broker operations require Linux"))
}
