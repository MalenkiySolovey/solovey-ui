package managementcontract_test

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	boundary "github.com/MalenkiySolovey/solovey-ui/internal/managementcontract"
	"github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
	"github.com/MalenkiySolovey/solovey-ui/service/update"
)

func TestActualDataOwnerChangeJobAndRecoveryProjection(t *testing.T) {
	for _, phase := range []string{"RESTORING", "APPLIED", "ROLLED_BACK", "RECOVERY_REQUIRED"} {
		t.Run(phase, func(t *testing.T) {
			operation := model.DataLifecycleOperation{OperationID: "data-operation:fixture", IdempotencyKey: "fixture", Kind: "RESTORE", State: phase, OwnerID: "core", Revision: 3,
				ExpectedRevision: strings.Repeat("a", 64), ManifestDigest: strings.Repeat("b", 64), BackupRef: strings.Repeat("c", 64), CreatedAt: 1700000000, UpdatedAt: 1700000001}
			if phase == "RECOVERY_REQUIRED" {
				operation.RestoredUntrusted = true
			}
			ownerChange := datalifecycle.ProjectChange(operation)
			ownerJob := datalifecycle.ProjectJob(operation)
			change, err := boundary.ProjectChange(ownerChange)
			if err != nil {
				t.Fatal(err)
			}
			job, err := boundary.ProjectJob(ownerJob)
			if err != nil {
				t.Fatal(err)
			}
			if change.Reference != job.Reference || change.Reference.Owner != "data" || change.Reference.ResourceID != "database" || change.Reference.OperationID != operation.OperationID || change.OwnerPhase != ownerChange.OwnerPhase || job.Outcome != change.Outcome || job.CancelSupported || job.CancelRequested != nil || job.Progress != nil {
				t.Fatalf("owner truth changed: %+v / %+v", change, job)
			}
			if phase == "ROLLED_BACK" && (!change.RollbackVerified || change.Outcome != "ROLLED_BACK") {
				t.Fatal("verified rollback lost")
			}
			if phase == "RECOVERY_REQUIRED" && (!change.RecoveryRequired || change.CheckpointRef != nil || change.Problem == nil || change.Problem.Code != "RECOVERY_REQUIRED") {
				t.Fatal("untrusted restored state retained acceptance/checkpoint authority")
			}
		})
	}
}

func TestActualUpdateOwnerProgressCriticalSectionsAndRecoveryProjection(t *testing.T) {
	for _, phase := range []string{"DOWNLOADING", "PREPARED", "ACTIVATING", "APPLIED", "ROLLED_BACK", "RECOVERY_REQUIRED"} {
		t.Run(phase, func(t *testing.T) {
			digest := strings.Repeat("a", 64)
			operation := model.UpdateOperation{OperationID: "update-operation:fixture", IdempotencyKey: "fixture", Revision: 7, Sequence: 1, ReleaseID: "fixture", Version: "2026.4.1", Channel: "main", ManifestDigest: digest, ArtifactSetDigest: digest, DeploymentRevision: digest, BrokerCapability: "broker-capabilities-1.3", MigrationSetDigest: digest, Platform: "linux", Arch: "amd64", BinaryProfile: "full", RestartClass: "stack", RebootClass: "operator-advisory", RollbackClass: "automatic", State: phase, BackupRef: digest, BytesTotal: 20, BytesCompleted: 10, CreatedAt: 1700000000, UpdatedAt: 1700000001}
			if phase == "RECOVERY_REQUIRED" {
				operation.RestoredUntrusted = true
			}
			ownerChange := update.ProjectChange(operation)
			ownerJob := update.ProjectJob(operation)
			change, err := boundary.ProjectChange(ownerChange)
			if err != nil {
				t.Fatal(err)
			}
			job, err := boundary.ProjectJob(ownerJob)
			if err != nil {
				t.Fatal(err)
			}
			if change.Reference != job.Reference || change.Reference.Owner != "update" || change.Reference.OperationID != operation.OperationID || change.OwnerPhase != ownerChange.OwnerPhase || job.CriticalSection != ownerJob.CriticalSection || job.CancelSupported || job.CancelRequested != nil {
				t.Fatalf("update truth changed: %+v / %+v", change, job)
			}
			if phase == "RECOVERY_REQUIRED" {
				if job.Progress != nil || change.CheckpointRef != nil || !change.RecoveryRequired {
					t.Fatal("restored/untrusted progress became verified")
				}
			} else if job.Progress == nil || job.Progress.Completed != 10 || job.Progress.Total != 20 || job.Progress.Unit != "bytes" {
				t.Fatal("invented or lost byte progress")
			}
			if phase == "ACTIVATING" && !job.CriticalSection {
				t.Fatal("activation critical section lost")
			}
			if phase == "APPLIED" && (job.Outcome != "COMMITTED" || job.ResultRef == nil || *job.ResultRef != operation.OperationID) {
				t.Fatal("durable acceptance/result lost")
			}
		})
	}
}
