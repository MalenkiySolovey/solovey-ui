package deployment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	"gorm.io/gorm"
)

func TestCheckpointCompletedPrepareRecoversCheckpointWithoutSecondHostMutation(t *testing.T) {
	manager, provider, db := deploymentFixture(t)
	preview, err := manager.Preview(context.Background(), domain.NativeHardened, true)
	if err != nil || !preview.Possible {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	if err := db.Exec(`CREATE TRIGGER checkpoint_fail_checkpoint_bind
		BEFORE UPDATE OF checkpoint_ref ON deployment_operations_v1
		WHEN OLD.checkpoint_ref = '' AND NEW.checkpoint_ref <> ''
		BEGIN SELECT RAISE(ABORT, 'checkpoint checkpoint bind interruption'); END`).Error; err != nil {
		t.Fatal(err)
	}
	request := StartRequest{TargetProfile: domain.NativeHardened, IdempotencyKey: "deployment-idem-checkpoint-handoff",
		ExpectedPreviewRevision: preview.Revision, ExpectedPostureRevision: preview.Posture.Revision, Acknowledged: true}
	if _, err := manager.Start(context.Background(), request); err == nil {
		t.Fatal("checkpoint bind interruption was not observed")
	}
	interrupted, err := manager.Repository.ByIdempotency(context.Background(), request.IdempotencyKey)
	if err != nil || interrupted.State != domain.StatePreflighted || interrupted.CheckpointRef != "" {
		t.Fatalf("interrupted=%#v err=%v", interrupted, err)
	}
	if err := db.Exec("DROP TRIGGER checkpoint_fail_checkpoint_bind").Error; err != nil {
		t.Fatal(err)
	}
	if err := manager.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	completed, err := manager.Repository.ByID(context.Background(), interrupted.OperationID)
	if err != nil || completed.State != domain.StateRolledBack || completed.CheckpointRef == "" || !completed.CheckpointReleased {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
	joined := strings.Join(provider.mutations, ",")
	if countMutationPrefix(provider.mutations, "prepare:native-hardened:") != 1 || countMutationPrefix(provider.mutations, "recover-prepare:native-hardened:") != 1 ||
		strings.Contains(joined, "apply:") || countMutationPrefix(provider.mutations, "rollback:native-legacy-root") != 1 || countMutationPrefix(provider.mutations, "release:") != 1 {
		t.Fatalf("cross-store replay mutations=%v", provider.mutations)
	}
}

func TestCheckpointStartupBeforePrepareCreatesOneRecoverableCheckpointThenRollsBack(t *testing.T) {
	manager, provider, _ := deploymentFixture(t)
	operation := checkpointOperation(provider.now, "before-prepare", domain.StatePreflighted)
	operation.Revision = 2
	operation.BindingRevision = domain.OperationBinding(operation)
	if err := manager.Repository.Create(context.Background(), operation, "interrupted_before_prepare"); err != nil {
		t.Fatal(err)
	}
	if err := manager.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	completed, err := manager.Repository.ByID(context.Background(), operation.OperationID)
	if err != nil || completed.State != domain.StateRolledBack || completed.CheckpointRef == "" || !completed.CheckpointReleased {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
	if countMutationPrefix(provider.mutations, "prepare:") != 0 || countMutationPrefix(provider.mutations, "recover-prepare:") != 1 ||
		countMutationPrefix(provider.mutations, "apply:") != 0 || countMutationPrefix(provider.mutations, "rollback:") != 1 ||
		countMutationPrefix(provider.mutations, "release:") != 1 {
		t.Fatalf("before-Prepare restart mutations=%v", provider.mutations)
	}
}

func TestCheckpointStartupAfterCheckpointBindDoesNotRepeatPrepareOrApply(t *testing.T) {
	manager, provider, _ := deploymentFixture(t)
	operation := checkpointOperation(provider.now, "after-checkpoint-bind", domain.StatePreflighted)
	operation.Revision = 3
	operation.CheckpointRef = domain.Revision("checkpoint:" + operation.OperationID)
	operation.BindingRevision = domain.OperationBinding(operation)
	if err := manager.Repository.Create(context.Background(), operation, "checkpoint_persisted"); err != nil {
		t.Fatal(err)
	}
	if err := manager.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	completed, err := manager.Repository.ByID(context.Background(), operation.OperationID)
	if err != nil || completed.State != domain.StateRolledBack || !completed.CheckpointReleased {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
	if countMutationPrefix(provider.mutations, "prepare:") != 0 || countMutationPrefix(provider.mutations, "recover-prepare:") != 0 ||
		countMutationPrefix(provider.mutations, "apply:") != 0 || countMutationPrefix(provider.mutations, "rollback:") != 1 ||
		countMutationPrefix(provider.mutations, "release:") != 1 {
		t.Fatalf("post-bind restart mutations=%v", provider.mutations)
	}
}

func TestCheckpointReleaseDebtRetriesAfterTerminalCommit(t *testing.T) {
	manager, provider, _ := deploymentFixture(t)
	preview, err := manager.Preview(context.Background(), domain.NativeHardened, true)
	if err != nil || !preview.Possible {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	operation, err := manager.Start(context.Background(), StartRequest{TargetProfile: domain.NativeHardened,
		IdempotencyKey: "deployment-idem-checkpoint-release", ExpectedPreviewRevision: preview.Revision,
		ExpectedPostureRevision: preview.Posture.Revision, Acknowledged: true})
	if err != nil || operation.State != domain.StateVerifying || strings.Contains(strings.Join(provider.mutations, ","), "release:") {
		t.Fatalf("nonterminal operation=%#v mutations=%v err=%v", operation, provider.mutations, err)
	}
	provider.releaseFail = true
	committed, err := manager.Confirm(context.Background(), ConfirmRequest{OperationID: operation.OperationID, ExpectedRevision: operation.Revision})
	if err != nil || committed.State != domain.StateCommitted || committed.CheckpointReleased {
		t.Fatalf("committed debt=%#v err=%v", committed, err)
	}
	provider.releaseFail = false
	if err := manager.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	released, err := manager.Repository.ByID(context.Background(), operation.OperationID)
	if err != nil || !released.CheckpointReleased || released.State != domain.StateCommitted {
		t.Fatalf("released=%#v err=%v", released, err)
	}
	if strings.Count(strings.Join(provider.mutations, ","), "release:") != 2 {
		t.Fatalf("release retry mutations=%v", provider.mutations)
	}
}

func TestCheckpointRestoreMakesTerminalHistoryInertAndUnresolvedAuthorityFailClosed(t *testing.T) {
	t.Run("terminal-history-inert", func(t *testing.T) {
		manager, provider, db := deploymentFixture(t)
		for index, state := range []domain.OperationState{domain.StateCommitted, domain.StateRolledBack} {
			operation := checkpointOperation(provider.now.Add(time.Duration(index)*time.Second), fmt.Sprintf("terminal-%d", index), state)
			operation.RestoredUntrusted = true
			operation.CheckpointRef = domain.Revision(operation.OperationID)
			row, err := operationRow(operation)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := manager.Repository.MarkRestoredUntrusted(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.Repository.Recovery(context.Background()); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("terminal restored history remained recovery-significant: %v", err)
		}
		var rows []model.DeploymentOperation
		if err := db.Order("operation_id asc").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.RestoredUntrusted || row.CheckpointRef != "" || !row.CheckpointReleased {
				t.Fatalf("terminal imported row was not inert: %#v", row)
			}
		}
	})

	t.Run("unresolved-fails-closed", func(t *testing.T) {
		manager, provider, db := deploymentFixture(t)
		operation := checkpointOperation(provider.now, "active", domain.StateApplying)
		operation.CheckpointRef = domain.Revision(operation.OperationID)
		row, err := operationRow(operation)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if err := manager.Repository.MarkRestoredUntrusted(context.Background()); err != nil {
			t.Fatal(err)
		}
		recovery, err := manager.Repository.Recovery(context.Background())
		if err != nil || recovery.OperationID != operation.OperationID || recovery.State != domain.StateManualRecoveryRequired ||
			!recovery.RestoredUntrusted || recovery.CheckpointRef != "" {
			t.Fatalf("restored unresolved=%#v err=%v", recovery, err)
		}
	})
}

func TestCheckpointTerminalRetentionExceedsLegacyCapacityAndPreservesProtectedClosures(t *testing.T) {
	manager, provider, db := deploymentFixture(t)
	if _, err := manager.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	oldestID := ""
	for index := 0; index < maxDeploymentOperations+20; index++ {
		operation := checkpointOperation(provider.now.Add(time.Duration(index)*time.Second), fmt.Sprintf("history-%03d", index), domain.StateCommitted)
		operation.CheckpointReleased = true
		row, err := operationRow(operation)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			oldestID = operation.OperationID
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.DeploymentJournal{OperationID: operation.OperationID, Sequence: operation.Revision,
			State: string(operation.State), Event: "terminal_fixture", Revision: domain.Revision(operation.OperationID), CreatedAt: operation.UpdatedAt}).Error; err != nil {
			t.Fatal(err)
		}
	}
	debt := checkpointOperation(provider.now.Add(time.Hour), "release-debt", domain.StateCommitted)
	debt.CheckpointRef = domain.Revision(debt.OperationID)
	debtRow, _ := operationRow(debt)
	if err := db.Create(&debtRow).Error; err != nil {
		t.Fatal(err)
	}
	manual := checkpointOperation(provider.now.Add(2*time.Hour), "manual", domain.StateManualRecoveryRequired)
	manual.RestoredUntrusted = true
	manualRow, _ := operationRow(manual)
	if err := db.Create(&manualRow).Error; err != nil {
		t.Fatal(err)
	}
	if err := manager.Repository.PruneHistory(context.Background(), provider.now.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var safeCount int64
	if err := db.Model(&model.DeploymentOperation{}).Where("checkpoint_released = ? AND state IN ?", true,
		[]string{string(domain.StateCommitted), string(domain.StateRolledBack)}).Count(&safeCount).Error; err != nil || safeCount != maxRetainedDeploymentHistory {
		t.Fatalf("retained safe history=%d err=%v", safeCount, err)
	}
	for _, id := range []string{debt.OperationID, manual.OperationID} {
		var count int64
		if err := db.Model(&model.DeploymentOperation{}).Where("operation_id = ?", id).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("protected closure %s count=%d err=%v", id, count, err)
		}
	}
	var oldCount, oldJournalCount int64
	_ = db.Model(&model.DeploymentOperation{}).Where("operation_id = ?", oldestID).Count(&oldCount).Error
	_ = db.Model(&model.DeploymentJournal{}).Where("operation_id = ?", oldestID).Count(&oldJournalCount).Error
	if oldCount != 0 || oldJournalCount != 0 {
		t.Fatalf("old safe closure survived operation=%d journal=%d", oldCount, oldJournalCount)
	}
	if err := db.Where("operation_id = ?", manual.OperationID).Delete(&model.DeploymentOperation{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.DeploymentOperation{}).Where("operation_id = ?", debt.OperationID).Update("checkpoint_released", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := manager.Repository.PruneHistory(context.Background(), provider.now.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	admitted := checkpointOperation(provider.now.Add(5*time.Hour), "new-admission", domain.StateDraft)
	if err := manager.Repository.Admit(context.Background(), admitted, "draft_created"); err != nil {
		t.Fatalf("new deployment admission remained exhausted: %v", err)
	}
}

func TestCheckpointProviderWithoutCheckpointLifecycleCannotPrepare(t *testing.T) {
	manager, provider, _ := deploymentFixture(t)
	manager.Provider = providerWithoutCheckpointLifecycle{Provider: provider}
	preview, err := manager.Preview(context.Background(), domain.NativeHardened, true)
	if err != nil || !preview.Possible {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	operation, err := manager.Start(context.Background(), StartRequest{TargetProfile: domain.NativeHardened,
		IdempotencyKey: "deployment-idem-checkpoint-no-lifecycle", ExpectedPreviewRevision: preview.Revision,
		ExpectedPostureRevision: preview.Posture.Revision, Acknowledged: true})
	if !errors.Is(err, ErrUnsafeMigration) || operation.State != domain.StateManualRecoveryRequired || len(provider.mutations) != 0 {
		t.Fatalf("operation=%#v mutations=%v err=%v", operation, provider.mutations, err)
	}
}

func TestCheckpointRestoredTerminalHistoryNearCapacityBecomesInertAndAdmitsNewWork(t *testing.T) {
	manager, provider, db := deploymentFixture(t)
	if _, err := manager.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxDeploymentOperations+20; index++ {
		operation := checkpointOperation(provider.now.Add(time.Duration(index)*time.Second), fmt.Sprintf("restored-history-%03d", index), domain.StateCommitted)
		operation.RestoredUntrusted = true
		operation.CheckpointRef = domain.Revision(operation.OperationID)
		row, err := operationRow(operation)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.Repository.MarkRestoredUntrusted(context.Background()); err != nil {
		t.Fatal(err)
	}
	var rows []model.DeploymentOperation
	if err := db.Order("updated_at desc, operation_id desc").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != maxRetainedDeploymentHistory {
		t.Fatalf("restored terminal history retained=%d", len(rows))
	}
	for _, row := range rows {
		if row.RestoredUntrusted || row.CheckpointRef != "" || !row.CheckpointReleased {
			t.Fatalf("restored terminal history retained authority: %#v", row)
		}
	}
	admitted := checkpointOperation(provider.now.Add(time.Hour), "after-restored-history", domain.StateDraft)
	if err := manager.Repository.Admit(context.Background(), admitted, "draft_created"); err != nil {
		t.Fatalf("restored terminal horizon blocked new admission: %v", err)
	}
}

func TestCheckpointTerminalRetentionHonorsAgeAndAggregateBytes(t *testing.T) {
	manager, provider, db := deploymentFixture(t)
	for _, fixture := range []struct {
		suffix string
		when   time.Time
	}{
		{suffix: "fresh", when: provider.now},
		{suffix: "aged", when: provider.now.Add(-deploymentHistoryRetentionAge - time.Second)},
		{suffix: "oversized", when: provider.now.Add(-time.Second)},
	} {
		operation := checkpointOperation(fixture.when, fixture.suffix, domain.StateCommitted)
		operation.CheckpointReleased = true
		row, err := operationRow(operation)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if fixture.suffix == "oversized" {
			journal := model.DeploymentJournal{OperationID: operation.OperationID, Sequence: operation.Revision,
				State: string(operation.State), Event: "terminal_fixture",
				Revision: domain.Revision(operation.OperationID), CreatedAt: operation.UpdatedAt}
			if err := db.Create(&journal).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("UPDATE deployment_journal_v1 SET reason = zeroblob(?) WHERE id = ?", maxDeploymentHistoryBytes+1, journal.ID).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := manager.Repository.PruneHistory(context.Background(), provider.now); err != nil {
		t.Fatal(err)
	}
	var rows []model.DeploymentOperation
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].OperationID != "deployment-operation:checkpoint-fresh" {
		t.Fatalf("age/byte retention result=%#v", rows)
	}
}

func TestCheckpointTimelineKeepsNewestBoundedDeterministicWindow(t *testing.T) {
	manager, provider, db := deploymentFixture(t)
	operation := checkpointOperation(provider.now, "timeline", domain.StateCommitted)
	operation.CheckpointReleased = true
	row, err := operationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	for sequence := uint64(1); sequence <= 80; sequence++ {
		journal := model.DeploymentJournal{OperationID: operation.OperationID, Sequence: sequence,
			State: string(operation.State), Event: fmt.Sprintf("event-%03d", sequence), Revision: domain.Revision(sequence), CreatedAt: int64(sequence)}
		if err := db.Create(&journal).Error; err != nil {
			t.Fatal(err)
		}
	}
	timeline, err := manager.Timeline(context.Background(), operation.OperationID)
	if err != nil || len(timeline) != 64 {
		t.Fatalf("timeline len=%d err=%v", len(timeline), err)
	}
	if timeline[0].Sequence != 17 || timeline[len(timeline)-1].Sequence != 80 {
		t.Fatalf("timeline first=%#v last=%#v", timeline[0], timeline[len(timeline)-1])
	}
	for index := 1; index < len(timeline); index++ {
		if timeline[index-1].Sequence >= timeline[index].Sequence {
			t.Fatalf("timeline is not deterministic chronological output at %d: %#v", index, timeline)
		}
	}
}

type providerWithoutCheckpointLifecycle struct{ Provider }

func countMutationPrefix(values []string, prefix string) int {
	count := 0
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			count++
		}
	}
	return count
}

func checkpointOperation(now time.Time, suffix string, state domain.OperationState) domain.Operation {
	operation := domain.Operation{Schema: domain.SchemaV1, OperationID: "deployment-operation:checkpoint-" + suffix,
		IdempotencyKey: "deployment-idem-checkpoint-" + suffix, State: state, FromProfile: domain.NativeLegacyRoot,
		TargetProfile: domain.NativeHardened, ExpectedPosture: domain.Revision("posture-" + suffix),
		ExpectedManagement: domain.Revision("management-" + suffix), Revision: 3, CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
	operation.BindingRevision = domain.OperationBinding(operation)
	return operation
}
