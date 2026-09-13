package sshmanagement

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
	"gorm.io/gorm"
)

func TestRetentionPrunesOnlyCompleteSafeTerminalClosures(t *testing.T) {
	manager, _, now := workflowFixture(t)
	db, _ := manager.Repository.db()
	for index := 0; index < maxTerminalSSHOperations+12; index++ {
		seedRetentionCandidateClosure(t, db, fmt.Sprintf("ssh-operation:retention-safe-%03d", index), domain.StateRolledBack, true, false, now.Add(-time.Duration(index)*time.Second))
	}
	seedRetentionCandidateClosure(t, db, "ssh-operation:retention-active", domain.StateStaged, false, false, *now)
	seedRetentionCandidateClosure(t, db, "ssh-operation:retention-manual", domain.StateManualRecoveryRequired, false, false, *now)
	seedRetentionCandidateClosure(t, db, "ssh-operation:retention-restored", domain.StateCommitted, true, true, *now)
	seedRetentionCandidateClosure(t, db, "ssh-operation:retention-broker-reference", domain.StateCommitted, false, false, *now)
	seedRetentionCandidateClosure(t, db, "ssh-operation:retention-live-evidence", domain.StateCommitted, true, false, *now)
	seedRetentionEvidence(t, db, "recovery:retention-live-reference", "ssh-operation:retention-live-evidence", "verified", 0, now.Add(time.Hour).Unix(), *now)
	seedRetentionCandidateClosure(t, db, "ssh-operation:retention-old", domain.StateRolledBack, true, false, now.Add(-sshTerminalRetentionAge-time.Second))
	seedRetentionCandidateClosure(t, db, "ssh-operation:retention-oversize", domain.StateRolledBack, true, false, *now)
	if err := db.Model(&model.SSHManagedArtifactCheckpoint{}).Where("operation_id = ?", "ssh-operation:retention-oversize").
		Update("prior_content", make([]byte, maxTerminalSSHBytes+1)).Error; err != nil {
		t.Fatal(err)
	}

	if err := manager.Repository.PruneHistory(context.Background(), *now); err != nil {
		t.Fatal(err)
	}
	var safeCount int64
	if err := db.Model(&model.SSHManagementCandidate{}).Where("operation_id LIKE ?", "ssh-operation:retention-safe-%").Count(&safeCount).Error; err != nil {
		t.Fatal(err)
	}
	if safeCount != maxTerminalSSHOperations {
		t.Fatalf("safe retained candidates=%d want=%d", safeCount, maxTerminalSSHOperations)
	}
	for _, operationID := range []string{"ssh-operation:retention-active", "ssh-operation:retention-manual", "ssh-operation:retention-restored", "ssh-operation:retention-broker-reference", "ssh-operation:retention-live-evidence"} {
		assertRetentionClosurePresent(t, db, operationID)
	}
	for _, operationID := range []string{"ssh-operation:retention-safe-139", "ssh-operation:retention-old", "ssh-operation:retention-oversize"} {
		assertRetentionClosureAbsent(t, db, operationID)
	}
	var retained []model.SSHManagementCandidate
	if err := db.Where("operation_id LIKE ?", "ssh-operation:retention-safe-%").Find(&retained).Error; err != nil {
		t.Fatal(err)
	}
	bytes := 0
	for _, row := range retained {
		size, err := terminalCandidateBytes(db, row)
		if err != nil {
			t.Fatal(err)
		}
		bytes += size
	}
	if bytes > maxTerminalSSHBytes {
		t.Fatalf("retained terminal logical bytes=%d horizon=%d", bytes, maxTerminalSSHBytes)
	}
}

func TestRetentionObservationAndEvidenceRetentionPreservesNewestAndLive(t *testing.T) {
	manager, _, now := workflowFixture(t)
	db, _ := manager.Repository.db()
	for index := 0; index < maxSSHPostureSnapshots+10; index++ {
		observed := now.Add(-time.Duration(index) * time.Second)
		row := model.SSHPostureSnapshot{SemanticRevision: domain.Revision(fmt.Sprintf("retention-posture-%d", index)),
			PayloadJSON: []byte(fmt.Sprintf(`{"generation":%d}`, index)), ObservedAt: observed.Unix(), ExpiresAt: observed.Add(time.Hour).Unix(), CreatedAt: observed.Unix()}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < maxTerminalSSHRecoveryRows+10; index++ {
		seedRetentionEvidence(t, db, fmt.Sprintf("recovery:retention-terminal-%03d", index), "", "invalidated", 0, now.Add(-time.Hour).Unix(), now.Add(-time.Duration(index)*time.Second))
	}
	seedRetentionEvidence(t, db, "recovery:retention-live", "", "verified", 0, now.Add(time.Hour).Unix(), now.Add(-sshObservationRetentionAge-time.Hour))

	if err := manager.Repository.PruneHistory(context.Background(), *now); err != nil {
		t.Fatal(err)
	}
	var postureCount, terminalEvidenceCount, liveCount int64
	if err := db.Model(&model.SSHPostureSnapshot{}).Count(&postureCount).Error; err != nil || postureCount != maxSSHPostureSnapshots {
		t.Fatalf("posture count=%d err=%v", postureCount, err)
	}
	if err := db.Model(&model.SSHRecoveryEvidence{}).Where("verification_state != ?", "verified").Count(&terminalEvidenceCount).Error; err != nil || terminalEvidenceCount != maxTerminalSSHRecoveryRows {
		t.Fatalf("terminal evidence count=%d err=%v", terminalEvidenceCount, err)
	}
	if err := db.Model(&model.SSHRecoveryEvidence{}).Where("id = ?", "recovery:retention-live").Count(&liveCount).Error; err != nil || liveCount != 1 {
		t.Fatalf("live evidence count=%d err=%v", liveCount, err)
	}
}

func TestRetentionPruneInterruptionRollsBackAndRestartRetryIsIdempotent(t *testing.T) {
	manager, _, now := workflowFixture(t)
	db, _ := manager.Repository.db()
	for index := 0; index < maxTerminalSSHOperations+1; index++ {
		seedRetentionCandidateClosure(t, db, fmt.Sprintf("ssh-operation:retention-fault-%03d", index), domain.StateRolledBack, true, false, now.Add(-time.Duration(index)*time.Second))
	}
	var before int64
	if err := db.Model(&model.SSHManagementCandidate{}).Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	const callback = "retention:interrupt-prune-delete"
	if err := db.Callback().Delete().Before("gorm:delete").Register(callback, func(tx *gorm.DB) { tx.AddError(errors.New("injected prune interruption")) }); err != nil {
		t.Fatal(err)
	}
	if err := manager.Repository.PruneHistory(context.Background(), *now); err == nil {
		t.Fatal("interrupted prune was acknowledged")
	}
	var afterFault int64
	if err := db.Model(&model.SSHManagementCandidate{}).Count(&afterFault).Error; err != nil || afterFault != before {
		t.Fatalf("partial prune survived interruption: before=%d after=%d err=%v", before, afterFault, err)
	}
	db.Callback().Delete().Remove(callback)
	if err := manager.Repository.PruneHistory(context.Background(), *now); err != nil {
		t.Fatal(err)
	}
	if err := manager.Repository.PruneHistory(context.Background(), *now); err != nil {
		t.Fatalf("restart retry was not idempotent: %v", err)
	}
	var afterRetry int64
	if err := db.Model(&model.SSHManagementCandidate{}).Count(&afterRetry).Error; err != nil || afterRetry != maxTerminalSSHOperations {
		t.Fatalf("retry count=%d err=%v", afterRetry, err)
	}
}

func TestRetentionTimelinePagingIsDeterministicAndBounded(t *testing.T) {
	manager, _, now := workflowFixture(t)
	db, _ := manager.Repository.db()
	operationID := "ssh-operation:retention-page"
	for sequence := uint64(1); sequence <= 9; sequence++ {
		row := model.SSHManagementJournal{OperationID: operationID, Sequence: sequence, State: string(domain.StateStaged),
			Event: "page_event", Revision: domain.Revision(sequence), CreatedAt: now.Unix()}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	first, err := manager.TimelinePage(context.Background(), operationID, 0, 4)
	if err != nil || len(first.Items) != 4 || !first.Truncated || first.NextAfter != 4 || first.ReplayLimit.TimelinePage != maxSSHTimelinePage {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	second, err := manager.TimelinePage(context.Background(), operationID, first.NextAfter, 4)
	if err != nil || len(second.Items) != 4 || !second.Truncated || second.Items[0].Sequence != 5 || second.NextAfter != 8 {
		t.Fatalf("second page=%#v err=%v", second, err)
	}
	last, err := manager.TimelinePage(context.Background(), operationID, second.NextAfter, 4)
	if err != nil || len(last.Items) != 1 || last.Truncated || last.Items[0].Sequence != 9 {
		t.Fatalf("last page=%#v err=%v", last, err)
	}
	if _, err := manager.TimelinePage(context.Background(), operationID, 0, maxSSHTimelinePage+1); err == nil {
		t.Fatal("oversized timeline page was accepted")
	}
}

func TestRetentionCommittedStageReleaseDebtSurvivesAndStartupClosesIt(t *testing.T) {
	manager, provider, _ := workflowFixture(t)
	started, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager)))
	if err != nil {
		t.Fatal(err)
	}
	provider.releaseStageFailure = true
	committed, err := manager.Confirm(context.Background(), ConfirmRequestV1{OperationID: started.Candidate.OperationID,
		ExpectedRevision: started.Candidate.Revision, ProviderEvidenceRef: started.Verifier})
	if err != nil || committed.State != domain.StateCommitted || committed.BrokerStageReleased || provider.releaseStageCalls != 1 {
		t.Fatalf("release debt=%#v calls=%d err=%v", committed, provider.releaseStageCalls, err)
	}
	provider.releaseStageFailure = false
	if err := recoveryRestartedManager(manager, provider).ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	closed, err := manager.Repository.Candidate(context.Background(), committed.OperationID)
	if err != nil || !closed.BrokerStageReleased || closed.Revision != committed.Revision+1 || provider.releaseStageCalls != 2 {
		t.Fatalf("closed debt=%#v calls=%d err=%v", closed, provider.releaseStageCalls, err)
	}
	if err := recoveryRestartedManager(manager, provider).ReconcileStartup(context.Background()); err != nil || provider.releaseStageCalls != 2 {
		t.Fatalf("release replay was not idempotent: calls=%d err=%v", provider.releaseStageCalls, err)
	}
}

func TestRetentionStageReleasePublicationInterruptionReplaysExactCleanup(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	started, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager)))
	if err != nil {
		t.Fatal(err)
	}
	db, _ := manager.Repository.db()
	conflict := model.SSHManagementJournal{OperationID: started.Candidate.OperationID, Sequence: started.Candidate.Revision + 2,
		State: string(domain.StateCommitted), Event: "conflict", Revision: domain.Revision("retention-release-publication-conflict"), CreatedAt: now.Unix()}
	if err := db.Create(&conflict).Error; err != nil {
		t.Fatal(err)
	}
	committed, err := manager.Confirm(context.Background(), ConfirmRequestV1{OperationID: started.Candidate.OperationID,
		ExpectedRevision: started.Candidate.Revision, ProviderEvidenceRef: started.Verifier})
	if err != nil || committed.BrokerStageReleased || provider.releaseStageCalls != 1 {
		t.Fatalf("interrupted release publication=%#v calls=%d err=%v", committed, provider.releaseStageCalls, err)
	}
	stored, err := manager.Repository.Candidate(context.Background(), committed.OperationID)
	if err != nil || stored.BrokerStageReleased || stored.Revision != committed.Revision {
		t.Fatalf("partially published release=%#v err=%v", stored, err)
	}
	if err := db.Delete(&conflict).Error; err != nil {
		t.Fatal(err)
	}
	if err := recoveryRestartedManager(manager, provider).ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	closed, err := manager.Repository.Candidate(context.Background(), committed.OperationID)
	if err != nil || !closed.BrokerStageReleased || provider.releaseStageCalls != 2 {
		t.Fatalf("replayed release=%#v calls=%d err=%v", closed, provider.releaseStageCalls, err)
	}
}

func seedRetentionCandidateClosure(t *testing.T, db *gorm.DB, operationID string, state domain.CandidateState, stageReleased, restored bool, updated time.Time) {
	t.Helper()
	row := model.SSHManagementCandidate{OperationID: operationID, Scope: "global", IdempotencyKey: "idem:" + operationID,
		EndpointID: "management:ssh:retention", State: string(state), Revision: 3, PolicyJSON: []byte(`{"schema":"retention"}`), PreservationJSON: []byte(`{"schema":"retention"}`),
		CandidateDigest: domain.Revision(operationID), BindingDigest: domain.Revision("binding:" + operationID),
		PostureRevision: domain.Revision("posture"), EndpointRevision: domain.Revision("endpoint"), RecoveryRevision: domain.Revision("recovery"),
		ProviderRevision: domain.Revision("provider"), BinaryRevision: domain.Revision("binary"), ServiceRevision: domain.Revision("service"),
		ConfigurationRevision: domain.Revision("configuration"), BrokerStageReleased: stageReleased, RestoredUntrusted: restored,
		ReasonCodesJSON: []byte(`[]`), CreatedAt: updated.Unix(), UpdatedAt: updated.Unix()}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SSHManagementJournal{OperationID: operationID, Sequence: 3, State: string(state), Event: "retention_terminal",
		Revision: domain.Revision(operationID + ":journal"), CreatedAt: updated.Unix()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SSHManagedArtifactCheckpoint{OperationID: operationID, PriorContent: []byte("prior"), PriorOwner: "root", PriorGroup: "root",
		PriorModeClass: "owner_read_write", PriorDigest: domain.Revision("prior"), StagedArtifactDigest: domain.Revision(operationID),
		StagedConfigurationRevision: domain.Revision("configuration"), CreatedAt: updated.Unix()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SSHReconnectChallenge{OperationID: operationID, CandidateDigest: row.CandidateDigest,
		MarkerDigest: domain.Revision(operationID + ":marker"), EndpointID: row.EndpointID, PrincipalID: "principal:retention", AuthenticationClass: "publickey",
		ServiceRevision: row.ServiceRevision, BinaryRevision: row.BinaryRevision, ConfigurationRevision: row.ConfigurationRevision,
		VerifierDigest: domain.Revision(operationID + ":verifier"), IssuedAt: updated.Unix(), ExpiresAt: updated.Add(time.Hour).Unix(), Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
}

func seedRetentionEvidence(t *testing.T, db *gorm.DB, id, target, state string, consumed, expires int64, updated time.Time) {
	t.Helper()
	row := model.SSHRecoveryEvidence{ID: id, Kind: "ssh", EndpointID: "management:ssh:retention", PrincipalID: "principal:retention",
		VerificationMethod: "retention", TargetOperation: target, VerifiedAt: updated.Unix(), ExpiresAt: expires,
		IndependenceClass: "independent", VerificationState: state, OperationBound: target != "", SingleUse: true, ConsumedAt: consumed,
		Revision: 1, ReasonCodesJSON: []byte(`[]`), SourceRevision: domain.Revision("source"), ConfigurationRevision: domain.Revision("configuration"),
		ProducerRevision: domain.Revision("producer"), UpdatedAt: updated.Unix()}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}

func assertRetentionClosurePresent(t *testing.T, db *gorm.DB, operationID string) {
	t.Helper()
	for _, value := range []any{&model.SSHManagementCandidate{}, &model.SSHManagementJournal{}, &model.SSHManagedArtifactCheckpoint{}, &model.SSHReconnectChallenge{}} {
		var count int64
		if err := db.Model(value).Where("operation_id = ?", operationID).Count(&count).Error; err != nil || count == 0 {
			t.Fatalf("protected closure %s missing %T count=%d err=%v", operationID, value, count, err)
		}
	}
}

func assertRetentionClosureAbsent(t *testing.T, db *gorm.DB, operationID string) {
	t.Helper()
	for _, value := range []any{&model.SSHManagementCandidate{}, &model.SSHManagementJournal{}, &model.SSHManagedArtifactCheckpoint{}, &model.SSHReconnectChallenge{}} {
		var count int64
		if err := db.Model(value).Where("operation_id = ?", operationID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("pruned closure %s retained %T count=%d err=%v", operationID, value, count, err)
		}
	}
}
