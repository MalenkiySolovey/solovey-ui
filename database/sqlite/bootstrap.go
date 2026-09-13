package sqlite

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/database/migration"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityvalidation "github.com/MalenkiySolovey/solovey-ui/internal/entities"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	passwordutil "github.com/MalenkiySolovey/solovey-ui/util/password"

	"gorm.io/gorm"
)

var adaptToCurrentVersion = adapt

func Init(dbPath string) (err error) {
	if err := prepareForInit(); err != nil {
		return err
	}
	if err := migration.MigratePathIfExists(dbPath, migration.Options{}); err != nil {
		return fmt.Errorf("migrate database before open: %w", err)
	}
	if err := preflightSupportedVersion(dbPath); err != nil {
		return err
	}
	if err := open(dbPath); err != nil {
		return err
	}
	initialized := false
	defer func() {
		if !initialized {
			if closeErr := Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("close failed database initialization: %w", closeErr))
			}
		}
	}()
	if err := db.AutoMigrate(schemaModels()...); err != nil {
		return err
	}
	if err := ensureSSHRetentionCompatibility(db); err != nil {
		return fmt.Errorf("ensure SSH retention compatibility: %w", err)
	}
	if err := ensureDeploymentRetentionCompatibility(db); err != nil {
		return fmt.Errorf("ensure deployment retention compatibility: %w", err)
	}
	if err := entityoutbounds.EnsureDefault(db); err != nil {
		return fmt.Errorf("ensure default outbound: %w", err)
	}
	if err := entitytls.EnsureSentinel(db); err != nil {
		return err
	}
	if err := entityvalidation.ValidateStored(db); err != nil {
		return fmt.Errorf("validate stored entities: %w", err)
	}
	if err := ensureIndexes(db); err != nil {
		return fmt.Errorf("ensure database indexes: %w", err)
	}
	if err := ensureInitialAdmin(dbPath); err != nil {
		return err
	}
	if err := adaptToCurrentVersion(); err != nil {
		return fmt.Errorf("post-migration adapt failed: %w", err)
	}
	if err := ensureSortOrders(); err != nil {
		return fmt.Errorf("sort-order backfill failed: %w", err)
	}
	initialized = true
	return nil
}

func ensureSSHRetentionCompatibility(database *gorm.DB) error {
	// A durable ROLLED_BACK transition means the managed artifact was exactly
	// restored. Older schemas had no explicit release bit; the restore itself
	// is therefore the compatibility proof that no stage authority remains.
	return database.Model(&model.SSHManagementCandidate{}).
		Where("state = ? AND broker_stage_released = ?", "ROLLED_BACK", false).
		Update("broker_stage_released", true).Error
}

func ensureDeploymentRetentionCompatibility(database *gorm.DB) error {
	// Rows without a checkpoint reference cannot carry broker cleanup debt.
	// This is idempotent and does not disturb a successfully released row that
	// retains its historical digest, or an older terminal row whose checkpoint
	// still needs the new typed release path.
	return database.Model(&model.DeploymentOperation{}).
		Where("checkpoint_ref = ? AND checkpoint_released = ?", "", false).
		Update("checkpoint_released", true).Error
}

func schemaModels() []any {
	return []any{
		&model.Setting{}, &model.Tls{}, &model.Inbound{}, &model.Outbound{},
		&model.Service{}, &model.Endpoint{}, &model.User{}, &model.Tokens{},
		&model.Stats{}, &model.ClientIP{}, &model.Client{}, &model.Changes{},
		&model.AuditEvent{}, &model.FailoverMemberState{}, &model.InboundDraft{},
		&model.ComponentMigration{}, &model.InboundFallbackCheckpoint{}, &model.InboundEndpointLease{},
		&model.AdminMFAFactor{}, &model.AdminRecoveryCode{}, &model.SecuritySession{}, &model.StepUpGrant{},
		&model.SSHPostureSnapshot{}, &model.SSHManagementCandidate{}, &model.SSHManagedArtifactCheckpoint{},
		&model.SSHReconnectChallenge{}, &model.SSHRecoveryEvidence{}, &model.SSHManagementJournal{},
		&model.DeploymentState{}, &model.DeploymentOperation{}, &model.DeploymentJournal{}, &model.DeploymentDoctorSnapshot{},
		&model.UpdateReleaseState{}, &model.UpdateOperation{}, &model.UpdateJournal{},
		&model.ResourcePressureState{}, &model.ResourcePressureTransition{}, &model.MigrationJournal{},
		&model.DataLifecycleOperation{}, &model.DataLifecycleJournal{},
	}
}

func ensureInitialAdmin(dbPath string) error {
	var count int64
	if err := db.Model(&model.User{}).Count(&count).Error; err != nil {
		return err
	}
	passwordPath := initialAdminPasswordPath(dbPath)
	if count != 0 {
		warnIfInitialAdminPasswordFileExists(passwordPath)
		return nil
	}

	password, err := common.SecureRandom(24)
	if err != nil {
		return err
	}
	passwordHash, err := passwordutil.Hash(context.Background(), password)
	if err != nil {
		return err
	}
	if err := writeInitialAdminPassword(passwordPath, password); err != nil {
		return err
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&model.User{
			Username:              "admin",
			Password:              passwordHash,
			ForcePasswordReset:    true,
			PasswordPolicyVersion: passwordutil.PolicyVersion,
			PasswordHashVersion:   passwordutil.PolicyVersion,
			CredentialGeneration:  1,
			MFAGeneration:         1,
		}).Error; err != nil {
			return err
		}
		// Fresh installations start with bounded sessions. Existing installations
		// deliberately receive legacy_unbounded from the 1.8 migration/default
		// fallback and require an explicit operator adoption.
		return tx.Exec(
			"INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
			settingcatalog.SessionLifetimePolicyKey,
			"bounded_v1",
		).Error
	}); err != nil {
		_ = os.Remove(passwordPath)
		return err
	}
	notifyInitialAdminPasswordSaved(passwordPath)
	return nil
}
