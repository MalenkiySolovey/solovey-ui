package backup

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

func TestRestoreIgnoresOnlyHistoricalPanelUpdateWithoutDurableData(t *testing.T) {
	t.Setenv(installstate.InstalledFileEnv, t.TempDir()+"/installed.json")
	previous := BackupOwnerManifest{
		ID: installstate.FormerPanelUpdateID, Available: true, Installed: true,
		Mode: "NO_DURABLE_DATA", ResourceManifest: &manifest.DurableResourceManifestV1{},
	}
	unknown := previous
	unknown.ID = "unknown-component"
	backup := BackupManifest{Owners: []BackupOwnerManifest{previous, unknown}}
	statuses := restoreOwnerCompatibility(backup)
	if len(statuses) != 1 || statuses[0].ID != unknown.ID || statuses[0].Available {
		t.Fatalf("historical owner or unknown owner incorrectly admitted: %+v", statuses)
	}
	if len(backup.Owners) != 2 || backup.Owners[0] != previous {
		t.Fatal("restore rewrote the verified manifest")
	}
	for _, altered := range []BackupManifest{
		{Owners: []BackupOwnerManifest{previous}, Tables: []BackupTableManifest{{Owner: previous.ID, Name: "unexpected"}}},
		{Owners: []BackupOwnerManifest{previous}, Files: &FileBackupManifest{Owners: []string{previous.ID}}},
	} {
		statuses := restoreOwnerCompatibility(altered)
		if len(statuses) != 1 || !statuses[0].Included || statuses[0].Available {
			t.Fatalf("historical owner unexpectedly admitted resources: %+v", statuses)
		}
	}
}
