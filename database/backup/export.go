package backup

import (
	"context"
	canonical "github.com/MalenkiySolovey/solovey-ui/backend/storage/backup"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	"github.com/MalenkiySolovey/solovey-ui/logger"
)

func backupExporter() canonical.Exporter {
	return canonical.Exporter{
		StagingDirectory: func() string { return configstorage.StagingPath("") },
		AppVersion:       configidentity.GetVersion,
		InstalledOwners:  installedBackupOwners,
		EnsureSentinel:   entitytls.EnsureSentinel,
		ExportOwnerFiles: exportOwnerFiles,
		Warning:          logger.Warning,
	}
}

func Export(exclude string) ([]byte, error) { return backupExporter().Export(exclude) }
func PrepareExport(exclude string) (string, func(), error) {
	return PrepareExportContext(context.Background(), exclude)
}
func PrepareExportContext(ctx context.Context, exclude string) (string, func(), error) {
	return backupExporter().PrepareExport(ctx, exclude)
}
func PrepareExportContextInDirectory(ctx context.Context, exclude, directory string) (string, func(), error) {
	return backupExporter().PrepareExportInDirectory(ctx, exclude, directory)
}
func ParseExcludes(exclude string) []string { return canonical.ParseExcludes(exclude) }

func cleanupBackupSidecars(path string) { canonical.CleanupSidecars(path) }
