package backup

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestDeploymentBackupPreservesSemanticHistoryAndScrubsLiveAuthority(t *testing.T) {
	dbPath := initBackupContributionDB(t)
	db := dbsqlite.DB()
	digest := strings.Repeat("a", 64)
	state := model.DeploymentState{Scope: "global", ProfileID: "native-hardened", DesiredProfile: "native-network-advanced",
		GeneratedProfile: "native-network-advanced", GeneratedRevision: digest, InstalledProfile: "native-hardened",
		ActiveProfile: "native-hardened", VerifiedProfile: "native-hardened", CompatibilityState: "HARDENED",
		DoctorRevision: digest, Runtime: "native", PostureRevision: digest, Trusted: true, ObservedAt: 1, UpdatedAt: 1}
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	operation := model.DeploymentOperation{OperationID: "deployment-operation:backup", IdempotencyKey: "deployment-idem-backup",
		State: "MANUAL_RECOVERY_REQUIRED", FromProfile: "native-legacy-root", TargetProfile: "native-hardened",
		ExpectedPosture: digest, ExpectedManagement: digest, CheckpointRef: digest, BrokerReceipt: digest, Revision: 5, RestoredUntrusted: true,
		CreatedAt: 1, UpdatedAt: 2, ReasonsJSON: []byte(`["rollback_verification_failed"]`), BindingRevision: digest}
	if err := db.Create(&operation).Error; err != nil {
		t.Fatal(err)
	}

	backupDB := openContributionBackup(t, dbPath)
	var copied model.DeploymentOperation
	if err := backupDB.Where("operation_id = ?", operation.OperationID).Take(&copied).Error; err != nil {
		t.Fatal(err)
	}
	if copied.CheckpointRef != "" || copied.BrokerReceipt != "" {
		t.Fatalf("live deployment authority leaked into backup: %#v", copied)
	}
	if copied.State != operation.State || string(copied.ReasonsJSON) != string(operation.ReasonsJSON) {
		t.Fatalf("safe deployment recovery metadata was not preserved: %#v", copied)
	}
	var copiedState model.DeploymentState
	if err := backupDB.Where("scope = ?", "global").Take(&copiedState).Error; err != nil {
		t.Fatal(err)
	}
	if copiedState.DesiredProfile != state.DesiredProfile || copiedState.GeneratedProfile != state.GeneratedProfile {
		t.Fatalf("semantic deployment projection was not preserved: %#v", copiedState)
	}
}
