//go:build linux && !minimal

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-backend/storage/durableowner"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

// Exercise the helper's own link closure: importing components in this test
// would conceal a missing production registration.
func TestPreservationExecutableContainsInstalledDurableOwners(t *testing.T) {
	paths, err := filepath.Glob("../../components/*/component.json")
	if err != nil || len(paths) == 0 {
		t.Fatal("component source inventory unavailable")
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var item manifest.Manifest
		if err := json.Unmarshal(data, &item); err != nil {
			t.Fatal(err)
		}
		if item.Delivery != manifest.DeliveryInProcess {
			continue
		}
		actual, ok := durableowner.Lookup(item.ID)
		// OpenWrt ships the extracted contribution packs, not every Full
		// Panel descriptor. Do not count headless source-only registrations.
		if len(item.Frontend.Entries) == 0 {
			if ok {
				t.Errorf("unpackaged owner %s is linked into the preservation executable", item.ID)
			}
			continue
		}
		if !ok || actual.Database.SchemaVersion != item.Normalized().Database.SchemaVersion {
			t.Errorf("installed owner %s is unavailable in preservation executable", item.ID)
		}
	}
}
