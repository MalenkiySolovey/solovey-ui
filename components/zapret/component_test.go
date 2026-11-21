//go:build !minimal

package zapret

import (
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/state"
)

func TestZapretAbsentAndDisabledStayInactive(t *testing.T) {
	item, registered := registry.ComponentByID("zapret")
	if !registered || item.Manifest.DefaultEnabled || item.PortLifecycle == nil {
		t.Fatal("expected the inert Zapret descriptor registered by Full")
	}
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	t.Setenv(installstate.ManagementEnv, "runtime")
	if items, err := state.Components(); err != nil || len(items) != 0 {
		t.Fatalf("registered but absent owner appeared in installed state: %+v, %v", items, err)
	}
	metadata := installstate.Metadata{Version: installstate.InstalledMetadataVersion, Profile: "full", Binary: "full",
		Components: []installstate.InstalledComponent{{ID: item.Manifest.ID, Delivery: item.Manifest.Delivery, Installed: true}}}
	if err := installstate.Store(path, metadata); err != nil {
		t.Fatal(err)
	}
	items, err := state.Components()
	if err != nil || len(items) != 1 || !items[0].AvailableInBinary || items[0].Enabled || items[0].Active ||
		items[0].ObservedLifecycle.Phase != state.LifecycleNotObserved {
		t.Fatalf("disabled owner was not inert: %+v, %v", items, err)
	}
}
