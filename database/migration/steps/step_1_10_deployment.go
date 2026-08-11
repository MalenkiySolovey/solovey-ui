package steps

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

func addDeploymentProfileSchema(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&model.DeploymentState{}, &model.DeploymentOperation{}, &model.DeploymentJournal{}, &model.DeploymentDoctorSnapshot{}, &model.SSHManagedArtifactCheckpoint{}); err != nil {
		return err
	}
	// MANUAL_RECOVERY_REQUIRED is deliberately unresolved: a second migration
	// cannot bypass retained rollback or recovery authority.
	return tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_deployment_single_unresolved
		ON deployment_operations_v1 ((1))
		WHERE state NOT IN ('COMMITTED','ROLLED_BACK')`).Error
}
