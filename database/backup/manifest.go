package backup

import (
	"context"
	canonical "github.com/MalenkiySolovey/solovey-ui/backend/storage/backup"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"gorm.io/gorm"
)

const (
	BackupManifestSchema = canonical.BackupManifestSchema
	BackupManifestTable  = canonical.BackupManifestTable
	MaxBackupTables      = canonical.MaxBackupTables
	MaxBackupManifest    = canonical.MaxBackupManifest
)

func installedBackupOwners() ([]string, error) {
	installed, err := installstate.InstalledComponents()
	if err != nil {
		return nil, err
	}
	result := make([]string, len(installed))
	for index, owner := range installed {
		result[index] = owner.ID
	}
	return result, nil
}

func writeBackupManifest(ctx context.Context, db *gorm.DB, tables []backupTable, excluded map[string]bool, files ...*FileBackupManifest) error {
	selected := make([]canonical.ManifestTable, len(tables))
	for index, table := range tables {
		selected[index] = canonical.ManifestTable{Name: table.name, Owner: table.owner, Opaque: table.opaque, AlwaysExclude: table.alwaysExclude, ExclusionCode: table.exclusionCode}
	}
	return canonical.WriteManifest(ctx, db, selected, excluded, canonical.ManifestFacts{AppVersion: configidentity.GetVersion(), InstalledOwners: installedBackupOwners}, files...)
}

func LoadAndVerifyManifest(ctx context.Context, db *gorm.DB) (BackupManifest, error) {
	return canonical.LoadAndVerifyManifest(ctx, db)
}
func digestBackupTable(ctx context.Context, db *gorm.DB, owner, table string) (BackupTableManifest, error) {
	return canonical.DigestTable(ctx, db, owner, table)
}
func backupManifestDigest(manifest BackupManifest) string { return canonical.DigestManifest(manifest) }
func excludedTableDigests(name, code string) (string, string) {
	return canonical.ExcludedTableDigests(name, code)
}
func validateSQLiteObjectInventory(ctx context.Context, db *gorm.DB, allowed map[string]bool, manifest bool) error {
	return canonical.ValidateObjectInventory(ctx, db, allowed, manifest)
}
func safeOwnerName(value string) bool { return canonical.ValidOwnerName(value) }
func verifyOwnerFiles(ctx context.Context, db *gorm.DB, manifest *FileBackupManifest) error {
	return canonical.VerifyOwnerFiles(ctx, db, manifest)
}
