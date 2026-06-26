package steps

import (
	"fmt"

	"gorm.io/gorm"
)

type step struct {
	fromMajor int
	fromMinor int
	target    string
	run       func(*gorm.DB) error
}

var sequentialSteps = []step{
	{fromMajor: 1, fromMinor: 2, target: "1.3", run: normalizeDNSAndOutboundOptions},
	{fromMajor: 1, fromMinor: 3, target: "1.4", run: addTokenAuditSchema},
	{fromMajor: 1, fromMinor: 4, target: "1.5", run: addClientIPPrivacySchema},
	{fromMajor: 1, fromMinor: 5, target: "1.6", run: addAuditFilterIndexes},
	{fromMajor: 1, fromMinor: 6, target: "1.7", run: addXUISyncSchema},
}

func RunPending(tx *gorm.DB, dbVersion string) (string, error) {
	if dbVersion == "" {
		if err := normalizeClientStorage(tx); err != nil {
			return "", fmt.Errorf("migration to 1.1: %w", err)
		}
		if err := importLegacyConfigObjects(tx); err != nil {
			return "", fmt.Errorf("migration to 1.2: %w", err)
		}
		dbVersion = "1.2"
	}
	for _, migrationStep := range sequentialSteps {
		if !dbVersionMinorIs(dbVersion, migrationStep.fromMajor, migrationStep.fromMinor) {
			continue
		}
		if err := migrationStep.run(tx); err != nil {
			return "", fmt.Errorf("migration to %s: %w", migrationStep.target, err)
		}
		dbVersion = migrationStep.target
	}
	return dbVersion, nil
}
