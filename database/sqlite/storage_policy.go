package sqlite

import (
	canonical "github.com/MalenkiySolovey/solovey-ui/backend/storage/bootstrap"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	dbschema "github.com/MalenkiySolovey/solovey-ui/database/schema"
	"github.com/MalenkiySolovey/solovey-ui/logger"
)

func storageInitializer() canonical.Initializer {
	return canonical.Initializer{
		DataDirectory:      configstorage.GetDBFolderPath(),
		DatabasePath:       configstorage.GetDBPath(),
		ApplicationVersion: configidentity.GetVersion(),
		CoreSchemaVersion:  dbschema.CurrentCoreVersion,
		CompareVersions:    versionpolicy.CompareVersions,
		Warning:            logger.Warning,
		Warningf:           logger.Warningf,
		Infof:              logger.Infof,
	}
}
