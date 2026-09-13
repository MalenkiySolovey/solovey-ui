package sqlite

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRetentionCompatibilityMarksOnlyCompletedRollbackReleased(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&model.SSHManagementCandidate{}); err != nil {
		t.Fatal(err)
	}
	rows := []model.SSHManagementCandidate{
		{OperationID: "ssh-operation:retention-legacy-rollback", IdempotencyKey: "idem:retention-legacy-rollback", State: "ROLLED_BACK", PolicyJSON: []byte(`{}`), PreservationJSON: []byte(`{}`), ReasonCodesJSON: []byte(`[]`)},
		{OperationID: "ssh-operation:retention-legacy-commit", IdempotencyKey: "idem:retention-legacy-commit", State: "COMMITTED", PolicyJSON: []byte(`{}`), PreservationJSON: []byte(`{}`), ReasonCodesJSON: []byte(`[]`)},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureSSHRetentionCompatibility(database); err != nil {
		t.Fatal(err)
	}
	var rolledBack, committed model.SSHManagementCandidate
	if err := database.Where("operation_id = ?", rows[0].OperationID).Take(&rolledBack).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Where("operation_id = ?", rows[1].OperationID).Take(&committed).Error; err != nil {
		t.Fatal(err)
	}
	if !rolledBack.BrokerStageReleased || committed.BrokerStageReleased {
		t.Fatalf("compatibility result rollback=%v committed=%v", rolledBack.BrokerStageReleased, committed.BrokerStageReleased)
	}
}

func TestCheckpointDeploymentRetentionCompatibilityPreservesCheckpointDebt(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&model.DeploymentOperation{}); err != nil {
		t.Fatal(err)
	}
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rows := []model.DeploymentOperation{
		{OperationID: "deployment-operation:checkpoint-no-checkpoint", IdempotencyKey: "idem:checkpoint-no-checkpoint", State: "COMMITTED", ReasonsJSON: []byte(`[]`)},
		{OperationID: "deployment-operation:checkpoint-release-debt", IdempotencyKey: "idem:checkpoint-release-debt", State: "COMMITTED", CheckpointRef: digest, ReasonsJSON: []byte(`[]`)},
		{OperationID: "deployment-operation:checkpoint-already-released", IdempotencyKey: "idem:checkpoint-already-released", State: "ROLLED_BACK", CheckpointRef: digest, CheckpointReleased: true, ReasonsJSON: []byte(`[]`)},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureDeploymentRetentionCompatibility(database); err != nil {
		t.Fatal(err)
	}
	var retained []model.DeploymentOperation
	if err := database.Order("operation_id asc").Find(&retained).Error; err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]model.DeploymentOperation, len(retained))
	for _, row := range retained {
		byID[row.OperationID] = row
	}
	if !byID[rows[0].OperationID].CheckpointReleased || byID[rows[1].OperationID].CheckpointReleased || !byID[rows[2].OperationID].CheckpointReleased {
		t.Fatalf("deployment compatibility result=%#v", byID)
	}
}
