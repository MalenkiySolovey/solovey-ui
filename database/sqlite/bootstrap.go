package sqlite

import (
	"context"
	"fmt"

	canonical "github.com/MalenkiySolovey/solovey-ui/backend/storage/bootstrap"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	storagesqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	configlogging "github.com/MalenkiySolovey/solovey-ui/config/logging"
	"github.com/MalenkiySolovey/solovey-ui/database/migration"
	entityvalidation "github.com/MalenkiySolovey/solovey-ui/internal/entities"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"

	"gorm.io/gorm"
)

var adaptToCurrentVersion = adaptDatabase

func Init(dbPath string) (err error) {
	return InitContext(context.Background(), dbPath)
}

// InitContext keeps a restore owner's temporary authority on local sessions.
// The published/cached base handle always has an ordinary background context.
func InitContext(ctx context.Context, dbPath string) error {
	return storagesqlite.InitializeContext(ctx, dbPath, storagesqlite.Bootstrap{
		Debug: configlogging.IsDebug(),
		BeforeOpen: func() error {
			if err := migration.MigratePathIfExists(dbPath, migration.Options{}); err != nil {
				return fmt.Errorf("migrate database before open: %w", err)
			}
			return preflightSupportedVersion(dbPath)
		},
		AfterOpen: func(database *gorm.DB) error { return initializeDatabase(database, dbPath) },
	})
}

func initializeDatabase(database *gorm.DB, dbPath string) error {
	if err := database.AutoMigrate(schemaModels()...); err != nil {
		return err
	}
	if err := ensureSSHRetentionCompatibility(database); err != nil {
		return fmt.Errorf("ensure SSH retention compatibility: %w", err)
	}
	if err := ensureDeploymentRetentionCompatibility(database); err != nil {
		return fmt.Errorf("ensure deployment retention compatibility: %w", err)
	}
	if err := entityoutbounds.EnsureDefault(database); err != nil {
		return fmt.Errorf("ensure default outbound: %w", err)
	}
	if err := entitytls.EnsureSentinel(database); err != nil {
		return err
	}
	if err := entityvalidation.ValidateStored(database); err != nil {
		return fmt.Errorf("validate stored entities: %w", err)
	}
	if err := ensureIndexes(database); err != nil {
		return fmt.Errorf("ensure database indexes: %w", err)
	}
	if err := ensureInitialAdmin(database, dbPath); err != nil {
		return err
	}
	if err := adaptToCurrentVersion(database); err != nil {
		return fmt.Errorf("post-migration adapt failed: %w", err)
	}
	if err := ensureSortOrders(database); err != nil {
		return fmt.Errorf("sort-order backfill failed: %w", err)
	}
	return nil
}

func ensureSSHRetentionCompatibility(database *gorm.DB) error {
	return canonical.EnsureSSHRetentionCompatibility(database)
}
func ensureDeploymentRetentionCompatibility(database *gorm.DB) error {
	return canonical.EnsureDeploymentRetentionCompatibility(database)
}
func schemaModels() []any { return model.SchemaModels() }
func ensureInitialAdmin(database *gorm.DB, dbPath string) error {
	return storageInitializer().EnsureInitialAdmin(database, dbPath)
}
