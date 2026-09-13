//go:build linux

package deploymentadapter

import (
	"errors"
	"testing"
)

func TestInstalledDockerRecoveryProjectionUsesProfileAuthority(t *testing.T) {
	t.Setenv("SUI_DEPLOYMENT_KIND", "docker")
	t.Setenv("SOLOVEY_DEPLOYMENT_PROFILE", "test-only-docker-profile")
	if _, err := LoadInstalledRecoveryProjection(); err == nil || errors.Is(err, ErrInstalledRecoveryProjectionUnavailable) {
		t.Fatalf("unknown Docker profile did not fail closed: %v", err)
	}
}
