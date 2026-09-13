package updatebroker

import (
	"strings"
	"testing"

	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func TestPackageManagedUpdateRegistersNoNativeHandlers(t *testing.T) {
	registry := broker.NewRegistry()
	if err := RegisterHandlers(registry, ModePackageManaged); err != nil {
		t.Fatal(err)
	}
	if got := registry.Verbs(broker.RolePanel); len(got) != 0 {
		t.Fatalf("package-managed update registered native verbs: %v", got)
	}
}

func TestUnknownUpdateModeFailsClosed(t *testing.T) {
	err := RegisterHandlers(broker.NewRegistry(), Mode("unknown"))
	if err == nil || !strings.Contains(err.Error(), "owner=update") || !strings.Contains(err.Error(), "adapter=unknown") {
		t.Fatalf("unknown update mode diagnostic = %v", err)
	}
}
