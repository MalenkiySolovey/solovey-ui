package deploymentbroker

import (
	"strings"
	"testing"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestPackageManagedDeploymentRegistersNoNativeHandlers(t *testing.T) {
	registry := broker.NewRegistry()
	if err := RegisterHandlers(registry, BackendPackageManaged, nil); err != nil {
		t.Fatal(err)
	}
	if got := registry.Verbs(broker.RolePanel); len(got) != 0 {
		t.Fatalf("package-managed deployment registered native verbs: %v", got)
	}
}

func TestUnknownDeploymentBackendFailsClosed(t *testing.T) {
	err := RegisterHandlers(broker.NewRegistry(), Backend("unknown"), nil)
	if err == nil || !strings.Contains(err.Error(), "owner=deployment") || !strings.Contains(err.Error(), "adapter=unknown") {
		t.Fatalf("unknown deployment backend diagnostic = %v", err)
	}
}
