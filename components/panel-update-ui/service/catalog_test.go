//go:build !minimal

package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	_ "github.com/MalenkiySolovey/solovey-ui/components/telegram"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestCatalogCombinesRuntimeStateWithReleaseManifest(t *testing.T) {
	dir := t.TempDir()
	installedPath := filepath.Join(dir, "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := installstate.Store(installedPath, installstate.Metadata{
		Version: 1,
		Profile: "full",
		Binary:  "full",
		Components: []installstate.InstalledComponent{
			{ID: "telegram", Delivery: manifest.DeliveryInProcess, Installed: true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	releasePath := filepath.Join(dir, "solovey-ui-release.json")
	if err := os.WriteFile(releasePath, []byte(`{
		"schemaVersion": 1,
		"components": {
			"telegram": {
				"name": "Telegram",
				"version": "2",
				"since": "2026.2.0",
				"delivery": "in-process",
				"defaultEnabled": true,
				"tokenScopes": ["telegram"]
			},
			"future-component": {
				"name": "Future Component",
				"version": "1",
				"since": "2026.3.0",
				"delivery": "in-process",
				"defaultEnabled": true
			}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	inventory, err := (Catalog{ReleaseManifestFile: releasePath}).Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Installed) != 1 || inventory.Installed[0].ID != "telegram" {
		t.Fatalf("installed = %#v", inventory.Installed)
	}
	if inventory.Installed[0].LatestVersion != "2" || inventory.Installed[0].Since != "2026.2.0" {
		t.Fatalf("installed release metadata was not merged: %#v", inventory.Installed[0])
	}
	if len(inventory.Unavailable) != 1 || inventory.Unavailable[0].ID != "future-component" {
		t.Fatalf("unavailable = %#v", inventory.Unavailable)
	}
	if inventory.Unavailable[0].Installable || inventory.Unavailable[0].AvailableInBinary {
		t.Fatalf("future component must not be installable from this binary: %#v", inventory.Unavailable[0])
	}
}
