package sshmanagement

import (
	"context"
	"errors"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
	"gorm.io/gorm"
)

func TestRecoveryCompletedStageRecoversCheckpointAndRollsBackExactlyOnce(t *testing.T) {
	for _, implementation := range []string{"openssh", "dropbear"} {
		t.Run(implementation, func(t *testing.T) {
			manager, provider, _ := workflowFixture(t)
			provider.posture.Binary.Implementation = implementation
			provider.posture.SemanticRevision = semanticPostureRevision(provider.posture)
			db, _ := manager.Repository.db()
			installRecoveryCheckpointFault(t, db)
			if _, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager))); err == nil {
				t.Fatal("checkpoint interruption was accepted")
			}
			candidate := recoveryActiveCandidate(t, manager)
			if candidate.State != domain.StatePreflighted || provider.stageCalls != 1 || provider.restoreMutations != 0 || provider.currentDigest != candidate.CandidateDigest {
				t.Fatalf("interrupted boundary candidate=%#v stage=%d restore_mutations=%d current=%s", candidate, provider.stageCalls, provider.restoreMutations, provider.currentDigest)
			}
			if _, err := manager.Repository.Checkpoint(context.Background(), candidate.OperationID); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("checkpoint unexpectedly durable: %v", err)
			}
			dropRecoveryCheckpointFault(t, db)
			restarted := recoveryRestartedManager(manager, provider)
			if err := restarted.ReconcileStartup(context.Background()); err != nil {
				t.Fatal(err)
			}
			rolledBack, err := restarted.Repository.Candidate(context.Background(), candidate.OperationID)
			if err != nil || rolledBack.State != domain.StateRolledBack || rolledBack.RollbackAttempts != 1 ||
				provider.restoreCalls != 1 || provider.restoreMutations != 1 || provider.currentDigest != provider.prior.Digest {
				t.Fatalf("recovered candidate=%#v calls=%d mutations=%d current=%s err=%v", rolledBack, provider.restoreCalls, provider.restoreMutations, provider.currentDigest, err)
			}
			if err := recoveryRestartedManager(manager, provider).ReconcileStartup(context.Background()); err != nil || provider.restoreCalls != 1 || provider.restoreMutations != 1 {
				t.Fatalf("duplicate restart replayed rollback: calls=%d mutations=%d err=%v", provider.restoreCalls, provider.restoreMutations, err)
			}
		})
	}
}

func TestRecoveryRollbackProviderReturnReplayDoesNotMutateTwice(t *testing.T) {
	manager, provider, _ := workflowFixture(t)
	db, _ := manager.Repository.db()
	installRecoveryCheckpointFault(t, db)
	if _, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager))); err == nil {
		t.Fatal("checkpoint interruption was accepted")
	}
	candidate := recoveryActiveCandidate(t, manager)
	dropRecoveryCheckpointFault(t, db)
	conflict := model.SSHManagementJournal{OperationID: candidate.OperationID, Sequence: 6, State: string(domain.StateRolledBack),
		Event: "conflict", Revision: domain.Revision("recovery-final-transition-conflict"), CreatedAt: candidate.UpdatedAt}
	if err := db.Create(&conflict).Error; err != nil {
		t.Fatal(err)
	}
	if err := recoveryRestartedManager(manager, provider).ReconcileStartup(context.Background()); err == nil {
		t.Fatal("post-restore database interruption was accepted")
	}
	interrupted, err := manager.Repository.Candidate(context.Background(), candidate.OperationID)
	if err != nil || interrupted.State != domain.StateRollbackPending || interrupted.RollbackAttempts != 1 || provider.restoreCalls != 1 || provider.restoreMutations != 1 {
		t.Fatalf("interrupted rollback=%#v calls=%d mutations=%d err=%v", interrupted, provider.restoreCalls, provider.restoreMutations, err)
	}
	if err := db.Delete(&conflict).Error; err != nil {
		t.Fatal(err)
	}
	if err := recoveryRestartedManager(manager, provider).ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	completed, err := manager.Repository.Candidate(context.Background(), candidate.OperationID)
	if err != nil || completed.State != domain.StateRolledBack || provider.restoreCalls != 2 || provider.restoreMutations != 1 || provider.currentDigest != provider.prior.Digest {
		t.Fatalf("replayed rollback=%#v calls=%d mutations=%d current=%s err=%v", completed, provider.restoreCalls, provider.restoreMutations, provider.currentDigest, err)
	}
}

func TestRecoveryMissingOrMismatchedCompletedStageFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*workflowProviderFake)
	}{
		{name: "missing", configure: func(provider *workflowProviderFake) { provider.recoverStageFailure = true }},
		{name: "mismatched", configure: func(provider *workflowProviderFake) { provider.recoverStageMismatch = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager, provider, _ := workflowFixture(t)
			db, _ := manager.Repository.db()
			installRecoveryCheckpointFault(t, db)
			if _, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager))); err == nil {
				t.Fatal("checkpoint interruption was accepted")
			}
			candidate := recoveryActiveCandidate(t, manager)
			dropRecoveryCheckpointFault(t, db)
			test.configure(provider)
			_ = recoveryRestartedManager(manager, provider).ReconcileStartup(context.Background())
			failed, err := manager.Repository.Candidate(context.Background(), candidate.OperationID)
			if err != nil || failed.State != domain.StateManualRecoveryRequired || provider.restoreMutations != 0 || provider.currentDigest != candidate.CandidateDigest {
				t.Fatalf("candidate=%#v mutations=%d current=%s err=%v", failed, provider.restoreMutations, provider.currentDigest, err)
			}
		})
	}
}

func TestRecoveryProviderWithoutCompletedStageAuthorityCannotMutate(t *testing.T) {
	manager, provider, _ := workflowFixture(t)
	manager.Provider = providerWithoutStageRecovery{Provider: provider}
	request := startFixture(previewFixture(t, manager))
	if _, err := manager.Start(context.Background(), request); err == nil {
		t.Fatal("provider without completed-stage authority was accepted")
	}
	candidate, err := manager.Repository.CandidateByIdempotency(context.Background(), request.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.State != domain.StateManualRecoveryRequired || provider.stageCalls != 0 || provider.restoreMutations != 0 || provider.currentDigest != provider.prior.Digest {
		t.Fatalf("candidate=%#v stage=%d mutations=%d current=%s", candidate, provider.stageCalls, provider.restoreMutations, provider.currentDigest)
	}
}

func TestRecoveryCheckpointReplayAcceptsOnlyTheExactCompletedGeneration(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	operationID := "ssh-operation:recovery-checkpoint-replay"
	stagedDigest := domain.Revision("recovery-staged")
	configurationRevision := domain.Revision("recovery-staged-configuration")
	if err := manager.Repository.SaveCheckpoint(context.Background(), operationID, provider.prior, stagedDigest, configurationRevision, *now); err != nil {
		t.Fatal(err)
	}
	if err := manager.Repository.SaveCheckpoint(context.Background(), operationID, provider.prior, stagedDigest, configurationRevision, now.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("exact checkpoint replay failed: %v", err)
	}
	mismatched := provider.prior
	mismatched.Digest = domain.Revision("different-prior")
	if err := manager.Repository.SaveCheckpoint(context.Background(), operationID, mismatched, stagedDigest, configurationRevision, *now); domain.ErrorCode(err) != domain.ReasonOperationStateConflict {
		t.Fatalf("mismatched checkpoint replay error=%v", err)
	}
}

type providerWithoutStageRecovery struct{ Provider }

func installRecoveryCheckpointFault(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`CREATE TRIGGER reject_recovery_checkpoint BEFORE INSERT ON ssh_managed_artifact_checkpoints_v1 BEGIN SELECT RAISE(ABORT, 'recovery checkpoint interruption'); END`).Error; err != nil {
		t.Fatal(err)
	}
}

func dropRecoveryCheckpointFault(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`DROP TRIGGER reject_recovery_checkpoint`).Error; err != nil {
		t.Fatal(err)
	}
}

func recoveryActiveCandidate(t *testing.T, manager *Manager) domain.CandidateV1 {
	t.Helper()
	candidate, err := manager.Repository.ActiveCandidate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func recoveryRestartedManager(original *Manager, provider Provider) *Manager {
	restarted := NewManager(original.Repository, provider)
	restarted.Now, restarted.Endpoints, restarted.Evidence = original.Now, original.Endpoints, original.Evidence
	return restarted
}
