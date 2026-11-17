package migration

import (
	"fmt"
	"strings"

	backendmigration "github.com/MalenkiySolovey/solovey-backend/storage/migration"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	"github.com/MalenkiySolovey/solovey-ui/database/migration/integrity"
	"github.com/MalenkiySolovey/solovey-ui/database/migration/steps"
	dbschema "github.com/MalenkiySolovey/solovey-ui/database/schema"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"

	"gorm.io/gorm"
)

type Options struct {
	RepairForeignKeyOrphans bool
	LegacyConfigPath        string
	ProjectRuntimeFiles     func(*gorm.DB, []byte) ([]byte, error)
	ReportFindings          func([]diagnostics.Finding)
}

// MigrateDb uses the configured Panel path; explicit candidate paths use the
// same canonical migration authority without changing the live configuration.
func MigrateDb() error { return MigrateDbWithOptions(Options{}) }
func MigrateDbWithOptions(options Options) error {
	return MigratePathIfExists(configstorage.GetDBPath(), options)
}
func MigratePathIfExists(path string, options Options) error {
	return backendmigration.MigratePathIfExists(path, migrationPlan(options), backendmigration.Options{LegacyConfigPath: options.LegacyConfigPath})
}
func MigratePath(path string, options Options) error {
	return backendmigration.MigratePath(path, migrationPlan(options), backendmigration.Options{LegacyConfigPath: options.LegacyConfigPath})
}

func migrationPlan(options Options) backendmigration.Plan {
	reportFindings := options.ReportFindings
	if reportFindings == nil {
		reportFindings = func(findings []diagnostics.Finding) {
			for _, finding := range findings {
				fmt.Printf("Migration candidate: %s [%s] %s: %s\n", finding.Path, finding.Code, finding.MigrationOutcome, finding.Message)
			}
		}
	}
	return backendmigration.Plan{
		ProductVersion:      configidentity.GetVersion(),
		CoreVersion:         dbschema.CurrentCoreVersion,
		CompareVersions:     versionpolicy.CompareVersions,
		Journal:             coreJournalContracts(),
		ValidateStoredState: validateStoredRules,
		VerifyIntegrity: func(tx *gorm.DB) error {
			if err := integrity.EnsureNoTLSForeignKeyParent(tx); err != nil {
				return err
			}
			return integrity.VerifyForeignKeysBeforeMigration(tx, integrity.Options{RepairForeignKeyOrphans: options.RepairForeignKeyOrphans})
		},
		MigrateLegacy: steps.RunPending,
		MigrateCore: func(tx *gorm.DB, version string) (string, error) {
			return steps.RunCorePendingWithOptions(tx, version, steps.CoreOptions{ProjectRuntimeFiles: options.ProjectRuntimeFiles, ReportFindings: reportFindings})
		},
	}
}

func readOnlyRuleUpgradePreflight(path string) error {
	return backendmigration.PreflightStoredState(path, validateStoredRules)
}
func validateStoredRules(database *gorm.DB) error {
	if !database.Migrator().HasTable("settings") {
		return nil
	}
	var value string
	if err := database.Table("settings").Select("value").Where("key = ?", "config").Scan(&value).Error; err != nil {
		return err
	}
	if strings.TrimSpace(value) == "" {
		return nil
	}
	base, err := singboxconfig.PrepareBaseOptionsUpgrade([]byte(value))
	if err != nil {
		return err
	}
	result, err := singboxvalidation.PrepareRuleUpgrade(base.Candidate)
	if err != nil {
		return err
	}
	for _, finding := range result.Findings {
		fmt.Printf("Rule upgrade: %s [%s]: %s\n", finding.Path, finding.Code, finding.Message)
	}
	return nil
}

type coreJournalContract = backendmigration.JournalContract

func coreJournalContracts() []coreJournalContract {
	return []coreJournalContract{
		{steps.OperationsLifecycleStepID, steps.OperationsLifecycleChecksum, "1.11"},
		{steps.SingBoxStateStepID, steps.SingBoxStateChecksum, "1.12"},
		{steps.ClientDisableReasonStepID, steps.ClientDisableReasonChecksum, "1.13"},
	}
}

func EnsureCurrentSchemaJournal(db *gorm.DB, seedFresh bool) error {
	return backendmigration.EnsureCurrentSchemaJournal(db, seedFresh, dbschema.CurrentCoreVersion, coreJournalContracts())
}
func ensureOperationsMigrationJournal(db *gorm.DB) error {
	return ensureCoreMigrationJournal(db, coreJournalContracts()[0])
}
func recordOperationsMigrationState(db *gorm.DB, state, errorCode string) error {
	return recordCoreMigrationState(db, coreJournalContracts()[0], state, errorCode)
}
func validateOperationsMigrationJournal(db *gorm.DB) error {
	return validateCoreMigrationJournal(db, coreJournalContracts()[0])
}
func ensureCoreMigrationJournal(db *gorm.DB, contract coreJournalContract) error {
	return backendmigration.EnsureCoreMigrationJournal(db, contract)
}
func recordCoreMigrationState(db *gorm.DB, contract coreJournalContract, state, errorCode string) error {
	return backendmigration.RecordCoreMigrationState(db, contract, state, errorCode)
}
func validateCoreMigrationJournal(db *gorm.DB, contract coreJournalContract) error {
	return backendmigration.ValidateCoreMigrationJournal(db, contract)
}
func validateCurrentMigrationJournals(db *gorm.DB) error {
	return backendmigration.ValidateCurrentMigrationJournals(db, coreJournalContracts())
}
func readVersionSetting(db *gorm.DB, key string) (string, error) {
	return backendmigration.ReadVersionSetting(db, key)
}
