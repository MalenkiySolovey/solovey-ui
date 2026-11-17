package backup

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-backend/storage/durableowner"
	dbsqlite "github.com/MalenkiySolovey/solovey-backend/storage/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	componentmanifest "github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestInstalledOwnerInventoryIncludesDisabledOwnerAndFailsClosedWhenUnavailable(t *testing.T) {
	const ownerID = "backup-disabled-fixture"
	const tableName = "backup_disabled_fixture_rows"
	durableowner.Register(componentmanifest.Manifest{ID: ownerID, Name: "Backup disabled fixture", Version: "1",
		Delivery: componentmanifest.DeliveryInProcess, DefaultEnabled: false,
		Database: componentmanifest.Database{Tables: []string{tableName}}})

	livePath := initBackupContributionDB(t)
	db := dbsqlite.DB()
	if err := db.Exec("CREATE TABLE " + tableName + " (id INTEGER PRIMARY KEY, value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	installedPath := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := installstate.Store(installedPath, installstate.Metadata{Version: 1, Components: []installstate.InstalledComponent{{
		ID: ownerID, Delivery: componentmanifest.DeliveryInProcess, Installed: true,
	}}}); err != nil {
		t.Fatal(err)
	}
	candidate := openContributionBackup(t, livePath)
	manifest, err := LoadAndVerifyManifest(context.Background(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, table := range manifest.Tables {
		if table.Owner == ownerID && table.Name == tableName {
			found = true
		}
	}
	if !found {
		t.Fatalf("disabled installed owner's table omitted from archive: %+v", manifest.Tables)
	}

	unavailablePath := filepath.Join(t.TempDir(), "installed-unavailable.json")
	t.Setenv(installstate.InstalledFileEnv, unavailablePath)
	if err := installstate.Store(unavailablePath, installstate.Metadata{Version: 1, Components: []installstate.InstalledComponent{{
		ID: "backup-unavailable-fixture", Delivery: componentmanifest.DeliveryInProcess, Installed: true,
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(""); err == nil ||
		!strings.Contains(err.Error(), "backup-unavailable-fixture") {
		t.Fatalf("unavailable installed owner did not fail closed: %v", err)
	}
}
