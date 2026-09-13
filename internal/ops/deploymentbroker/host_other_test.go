//go:build !linux

package deploymentbroker

import (
	"strings"
	"testing"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestNativeSystemdDeploymentMutationIsUnavailableOffLinux(t *testing.T) {
	err := RegisterHandlers(broker.NewRegistry(), BackendSystemdNative, nil)
	if err == nil || !strings.Contains(err.Error(), "linux_required") {
		t.Fatalf("non-Linux Systemd deployment registration=%v", err)
	}
}
