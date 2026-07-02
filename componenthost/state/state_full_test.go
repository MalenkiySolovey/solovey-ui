//go:build !minimal

package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	_ "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions"
	_ "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions"
	_ "github.com/MalenkiySolovey/solovey-ui/components/telegram"
)

func TestComponentsEmptyWhenMetadataIsAbsent(t *testing.T) {
	t.Setenv(installstate.InstalledFileEnv, filepath.Join(t.TempDir(), "installed.json"))

	components, err := Components()
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 0 {
		t.Fatalf("expected no implicit components, got %d: %#v", len(components), components)
	}
}

func TestComponentsReturnOnlyInstalledMetadataEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"binary": "full",
		"components": [
			{"id": "telegram", "delivery": "in-process", "installed": true}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	components, err := Components()
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 1 || components[0].ID != "telegram" {
		t.Fatalf("expected only installed telegram component, got %#v", components)
	}
	if !components[0].Installed || !components[0].Enabled || !components[0].Active {
		t.Fatalf("installed default-enabled component should be active: %#v", components[0])
	}
}
