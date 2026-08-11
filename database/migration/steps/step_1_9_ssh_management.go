package steps

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

func addSSHManagementRecoverySchema(tx *gorm.DB) error {
	models := []any{
		&model.SSHPostureSnapshot{}, &model.SSHManagementCandidate{}, &model.SSHManagedArtifactCheckpoint{},
		&model.SSHReconnectChallenge{}, &model.SSHRecoveryEvidence{}, &model.SSHManagementJournal{},
	}
	if err := tx.AutoMigrate(models...); err != nil {
		return err
	}
	if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_ssh_management_single_active
		ON ssh_management_candidates_v1(scope)
		WHERE state NOT IN ('COMMITTED','ROLLED_BACK','MANUAL_RECOVERY_REQUIRED')`).Error; err != nil {
		return err
	}
	if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_ssh_recovery_operation_state
		ON ssh_recovery_evidence_v1(target_operation, verification_state, expires_at)`).Error; err != nil {
		return err
	}
	return nil
}
