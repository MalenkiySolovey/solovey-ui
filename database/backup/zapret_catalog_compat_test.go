package backup

import (
	"testing"

	componentmanifest "github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation/catalogstore"
)

func TestHistoricalZapretCatalogOwnershipDoesNotAdmitUndeclaredResources(t *testing.T) {
	contract := componentmanifest.Database{SchemaVersion: "2", MigrationVersion: "2", Tables: []string{catalogstore.Table}, Settings: []string{zapretactivation.CatalogKey}}
	contract.CompatibilityRange.MinimumSchema = "1"
	owner := BackupOwnerManifest{ID: "zapret", Mode: "NO_DURABLE_DATA", ResourceManifest: &componentmanifest.Database{}}
	var archive BackupManifest
	if !historicalZapretCatalogOwner(archive, owner, contract) {
		t.Fatal("original Zapret setting-only archive was not recognized")
	}
	archive.Tables = []BackupTableManifest{{Owner: "other", Name: catalogstore.Table}}
	if historicalZapretCatalogOwner(archive, owner, contract) {
		t.Fatal("pre-v2 Zapret owner allowed to smuggle in the normalized table")
	}
}
