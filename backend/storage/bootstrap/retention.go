package bootstrap

import (
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"gorm.io/gorm"
)

func EnsureSSHRetentionCompatibility(database *gorm.DB) error {
	// A durable ROLLED_BACK transition means the managed artifact was exactly
	// restored. Older schemas had no explicit release bit; the restore itself
	// is therefore the compatibility proof that no stage authority remains.
	return database.Model(&model.SSHManagementCandidate{}).
		Where("state = ? AND broker_stage_released = ?", "ROLLED_BACK", false).
		Update("broker_stage_released", true).Error
}

func EnsureDeploymentRetentionCompatibility(database *gorm.DB) error {
	// Rows without a checkpoint reference cannot carry broker cleanup debt.
	// This is idempotent and does not disturb a successfully released row that
	// retains its historical digest, or an older terminal row whose checkpoint
	// still needs the new typed release path.
	return database.Model(&model.DeploymentOperation{}).
		Where("checkpoint_ref = ? AND checkpoint_released = ?", "", false).
		Update("checkpoint_released", true).Error
}
