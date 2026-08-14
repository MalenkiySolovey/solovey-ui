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
	{fromMajor: 1, fromMinor: 6, target: "1.7", run: completeComponentSchemaBoundary},
}

var coreSequentialSteps = []step{
	{fromMajor: 1, fromMinor: 7, target: "1.8", run: addPanelNativeSecuritySchema},
	{fromMajor: 1, fromMinor: 8, target: "1.9", run: addSSHManagementRecoverySchema},
	{fromMajor: 1, fromMinor: 9, target: "1.10", run: addDeploymentProfileSchema},
	{fromMajor: 1, fromMinor: 10, target: "1.11", run: addOperationsLifecycleSchema},
}

func RunPending(tx *gorm.DB, dbVersion string, legacyConfig []byte) (string, error) {
	if dbVersion == "" {
		if err := normalizeClientStorage(tx); err != nil {
			return "", fmt.Errorf("migration to 1.1: %w", err)
		}
		if err := importLegacyConfigObjects(tx, legacyConfig); err != nil {
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

func RunCorePending(tx *gorm.DB, coreVersion string) (string, error) {
	if coreVersion == "" {
		coreVersion = "1.7"
	}
	for _, migrationStep := range coreSequentialSteps {
		if !dbVersionMinorIs(coreVersion, migrationStep.fromMajor, migrationStep.fromMinor) {
			continue
		}
		if err := migrationStep.run(tx); err != nil {
			return "", fmt.Errorf("core migration to %s: %w", migrationStep.target, err)
		}
		coreVersion = migrationStep.target
	}
	if coreVersion != "1.11" {
		return "", fmt.Errorf("core schema %q is outside the supported sequential migration plan", coreVersion)
	}
	return coreVersion, nil
}
