package service

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestInstallerOwnedInventoryStillAllowsEnabledPolicy(t *testing.T) {
	t.Setenv(installstate.ManagementEnv, "installer")
	for _, installed := range []bool{true, false} {
		status := statusForManifest(manifest.Manifest{ID: "telegram"}, installed, true)
		if status.Installable || status.Removable || status.Locked || !status.Enabled || status.Installed != installed {
			t.Fatalf("incorrect installer-managed capabilities: %+v", status)
		}
		if status.LockedReason != installstate.ErrPackageManaged.Error() {
			t.Fatal("missing bounded lifecycle reason")
		}
	}
}

func TestRuntimeManagerProtectsUpdateComponentFromSelfManagement(t *testing.T) {
	manager := RuntimeManager{}

	if _, err := manager.Disable(OperationContext{}, UpdateComponentID); err == nil || !strings.Contains(err.Error(), "cannot disable itself") {
		t.Fatalf("Disable(update component) error = %v, want self-disable rejection", err)
	}
	if _, err := manager.Remove(OperationContext{}, UpdateComponentID); err == nil || !strings.Contains(err.Error(), "cannot remove itself") {
		t.Fatalf("Remove(update component) error = %v, want self-remove rejection", err)
	}
}
