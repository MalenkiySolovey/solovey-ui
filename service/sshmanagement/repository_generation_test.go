package sshmanagement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
	"gorm.io/gorm"
)

func TestGenerationSameSemanticPostureRefreshesNewestObservation(t *testing.T) {
	manager, _, now := workflowFixture(t)
	older := postureFixture(*now)
	if err := manager.Repository.SavePosture(context.Background(), older, *now); err != nil {
		t.Fatal(err)
	}
	newer := older
	newer.ObservedAt = now.Add(time.Minute).Unix()
	newer.ExpiresAt = now.Add(4 * time.Minute).Unix()
	if newer.SemanticRevision != older.SemanticRevision {
		t.Fatal("fixture unexpectedly changed semantic identity")
	}
	if err := manager.Repository.SavePosture(context.Background(), newer, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := manager.Repository.LatestPosture(context.Background())
	if err != nil || !reflect.DeepEqual(got, newer) {
		t.Fatalf("same-semantic refresh was not current: got=%#v err=%v", got, err)
	}
	if err := manager.Repository.SavePosture(context.Background(), older, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err = manager.Repository.LatestPosture(context.Background())
	if err != nil || !reflect.DeepEqual(got, newer) {
		t.Fatalf("older observation replaced newer generation: got=%#v err=%v", got, err)
	}
}

func TestGenerationRecoveryEvidenceUpsertReplacesOneCompleteGeneration(t *testing.T) {
	manager, _, now := workflowFixture(t)
	initial := generationRecoveryPath("generation", hostresources.ManagementPanel, *now)
	initial.VerificationState = "invalidated"
	initial.ReasonCodes = []string{"old_generation"}
	if err := manager.Repository.UpsertRecoveryEvidence(context.Background(), initial, *now); err != nil {
		t.Fatal(err)
	}
	replacement := generationRecoveryPath("generation", hostresources.ManagementSSH, now.Add(time.Minute))
	replacement.EndpointID = "management:ssh:replacement"
	replacement.PrincipalID = "principal:replacement"
	replacement.SourcePrefix = "192.0.2.44/32"
	replacement.VerificationMethod = "fresh_ssh_login"
	replacement.EvidenceProvider = "replacement-provider"
	replacement.TargetOperation = "ssh-operation:replacement"
	replacement.IndependenceClass = "independent"
	replacement.OperationBound = true
	replacement.SingleUse = true
	replacement.Revision = 7
	replacement.SourceRevision = domain.Revision("source-replacement")
	replacement.ConfigurationRevision = domain.Revision("configuration-replacement")
	replacement.ServiceRevision = domain.Revision("service-replacement")
	replacement.BinaryRevision = domain.Revision("binary-replacement")
	if err := manager.Repository.UpsertRecoveryEvidence(context.Background(), replacement, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	want, _ := recoveryEvidenceRow(replacement, now.Add(time.Minute))
	var got model.SSHRecoveryEvidence
	db, _ := manager.Repository.db()
	if err := db.Where("id = ?", replacement.ID).Take(&got).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed evidence generations:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestGenerationRecoveryEvidenceLiveCapacityIsExplicitAtWriteAndRead(t *testing.T) {
	manager, _, now := workflowFixture(t)
	db, _ := manager.Repository.db()
	seedGenerationLiveRows(t, db, *now, maxLiveRecoveryEvidence-1, "capacity")
	contenders := []hostresources.RecoveryPathV1{
		generationRecoveryPath("capacity-last-a", hostresources.ManagementPanel, *now),
		generationRecoveryPath("capacity-last-b", hostresources.ManagementPanel, *now),
	}
	results := make(chan error, len(contenders))
	for _, contender := range contenders {
		contender := contender
		go func() { results <- manager.Repository.UpsertRecoveryEvidence(context.Background(), contender, *now) }()
	}
	var accepted, rejected int
	for range contenders {
		switch err := <-results; {
		case err == nil:
			accepted++
		case errors.Is(err, errRecoveryEvidenceCapacity):
			rejected++
		default:
			t.Fatalf("concurrent capacity admission returned %v", err)
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("concurrent boundary accepted=%d rejected=%d", accepted, rejected)
	}
	var admittedContenders int64
	if err := db.Model(&model.SSHRecoveryEvidence{}).Where("id IN ?", []string{contenders[0].ID, contenders[1].ID}).Count(&admittedContenders).Error; err != nil || admittedContenders != 1 {
		t.Fatalf("capacity transaction leaked: contenders=%d err=%v", admittedContenders, err)
	}
	got, err := manager.Repository.RecoveryRows(context.Background(), *now)
	if err != nil || len(got) != maxLiveRecoveryEvidence {
		t.Fatalf("bounded read rows=%d err=%v", len(got), err)
	}
	overflow := generationRecoveryPath("capacity-imported-overflow", hostresources.ManagementPanel, *now)
	overflowRow, _ := recoveryEvidenceRow(overflow, *now)
	if err := db.Create(&overflowRow).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Repository.RecoveryRows(context.Background(), *now); !errors.Is(err, errRecoveryEvidenceCapacity) {
		t.Fatalf("oversized imported inventory error=%v, want explicit capacity", err)
	}
}

func TestGenerationCommitEvidenceCapacityFailureRollsBackWholeCommit(t *testing.T) {
	manager, _, now := workflowFixture(t)
	started, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager)))
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := manager.Repository.Challenge(context.Background(), started.Candidate.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	db, _ := manager.Repository.db()
	seedGenerationLiveRows(t, db, *now, maxLiveRecoveryEvidence, "commit-capacity")
	next := started.Candidate
	next.State, next.Revision, next.ReconciledAt = domain.StateCommitted, next.Revision+1, now.Unix()
	path := generationRecoveryPath("commit-capacity-overflow", hostresources.ManagementSSH, *now)
	path.VerificationMethod = "fresh_ssh_login"
	path.IndependenceClass = "independent_reconnect"
	path.SingleUse = true
	if err := manager.Repository.CommitCandidateCASWithJournalAndEvidence(context.Background(), next, started.Candidate.Revision,
		started.Candidate.State, challenge.Revision, path, *now); !errors.Is(err, errRecoveryEvidenceCapacity) {
		t.Fatalf("commit capacity error=%v, want explicit capacity", err)
	}
	stored, err := manager.Repository.Candidate(context.Background(), started.Candidate.OperationID)
	if err != nil || stored.State != started.Candidate.State || stored.Revision != started.Candidate.Revision {
		t.Fatalf("capacity failure leaked candidate commit: candidate=%#v err=%v", stored, err)
	}
	storedChallenge, err := manager.Repository.Challenge(context.Background(), started.Candidate.OperationID)
	if err != nil || storedChallenge.ConsumedAt != 0 || storedChallenge.Revision != challenge.Revision {
		t.Fatalf("capacity failure consumed challenge: challenge=%#v err=%v", storedChallenge, err)
	}
	var evidenceCount int64
	if err := db.Model(&model.SSHRecoveryEvidence{}).Where("id = ?", path.ID).Count(&evidenceCount).Error; err != nil || evidenceCount != 0 {
		t.Fatalf("capacity failure leaked evidence: count=%d err=%v", evidenceCount, err)
	}
}

func TestGenerationRestoreInvalidatesAllHostLocalAuthorityUntilReobserved(t *testing.T) {
	manager, _, now := workflowFixture(t)
	posture := postureFixture(*now)
	if err := manager.Repository.SavePosture(context.Background(), posture, *now); err != nil {
		t.Fatal(err)
	}
	kinds := []hostresources.ManagementServiceKind{hostresources.ManagementPanel, hostresources.ManagementSSH, hostresources.ManagementSubscriptionAdmin, hostresources.ManagementOtherAdmin}
	for index, kind := range kinds {
		path := generationRecoveryPath(fmt.Sprintf("restore-%d", index), kind, *now)
		if err := manager.Repository.UpsertRecoveryEvidence(context.Background(), path, *now); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.Repository.MarkRestoredUntrusted(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Repository.LatestPosture(context.Background()); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("imported posture remained authoritative: %v", err)
	}
	if rows, err := manager.Repository.RecoveryRows(context.Background(), *now); err != nil || len(rows) != 0 {
		t.Fatalf("imported recovery remained live: rows=%d err=%v", len(rows), err)
	}
	var raw []model.SSHRecoveryEvidence
	db, _ := manager.Repository.db()
	if err := db.Order("id asc").Find(&raw).Error; err != nil || len(raw) != len(kinds) {
		t.Fatalf("descriptive rows=%d err=%v", len(raw), err)
	}
	for _, row := range raw {
		var reasons []string
		if row.VerificationState != "invalidated" || row.Revision != 2 || json.Unmarshal(row.ReasonCodesJSON, &reasons) != nil ||
			len(reasons) != 1 || reasons[0] != string(domain.ReasonRestoredStateUntrusted) {
			t.Fatalf("restored evidence retained authority: %#v reasons=%v", row, reasons)
		}
	}
	refreshed := postureFixture(now.Add(time.Minute))
	if err := manager.Repository.SavePosture(context.Background(), refreshed, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	reverified := generationRecoveryPath("restore-0", hostresources.ManagementPanel, now.Add(time.Minute))
	reverified.Revision = 3
	if err := manager.Repository.UpsertRecoveryEvidence(context.Background(), reverified, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	paths, err := manager.RecoveryPaths(context.Background(), now.Add(time.Minute))
	if err != nil || len(paths) != 1 || paths[0].ID != reverified.ID || paths[0].VerifiedAt != reverified.VerifiedAt {
		t.Fatalf("local re-verification did not publish one fresh generation: paths=%#v err=%v", paths, err)
	}
}

func TestGenerationRestoreTrustRevocationIsAtomic(t *testing.T) {
	manager, _, now := workflowFixture(t)
	posture := postureFixture(*now)
	path := generationRecoveryPath("restore-atomic", hostresources.ManagementPanel, *now)
	if err := manager.Repository.SavePosture(context.Background(), posture, *now); err != nil {
		t.Fatal(err)
	}
	if err := manager.Repository.UpsertRecoveryEvidence(context.Background(), path, *now); err != nil {
		t.Fatal(err)
	}
	db, _ := manager.Repository.db()
	if err := db.Exec(`CREATE TRIGGER reject_generation_restore BEFORE UPDATE ON ssh_recovery_evidence_v1 BEGIN SELECT RAISE(ABORT, 'generation restore fault'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := manager.Repository.MarkRestoredUntrusted(context.Background(), now.Add(time.Minute)); err == nil {
		t.Fatal("restore revocation fault was accepted")
	}
	var postureCount int64
	if err := db.Model(&model.SSHPostureSnapshot{}).Count(&postureCount).Error; err != nil || postureCount != 1 {
		t.Fatalf("failed restore revocation partially deleted posture: count=%d err=%v", postureCount, err)
	}
	var evidence model.SSHRecoveryEvidence
	if err := db.Where("id = ?", path.ID).Take(&evidence).Error; err != nil || evidence.VerificationState != "verified" || evidence.Revision != path.Revision {
		t.Fatalf("failed restore revocation partially invalidated evidence: row=%#v err=%v", evidence, err)
	}
}

func seedGenerationLiveRows(t *testing.T, db *gorm.DB, now time.Time, count int, prefix string) {
	t.Helper()
	rows := make([]model.SSHRecoveryEvidence, 0, count)
	for index := 0; index < count; index++ {
		row, err := recoveryEvidenceRow(generationRecoveryPath(fmt.Sprintf("%s-%04d", prefix, index), hostresources.ManagementPanel, now), now)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	if err := db.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
}

func generationRecoveryPath(id string, kind hostresources.ManagementServiceKind, now time.Time) hostresources.RecoveryPathV1 {
	return hostresources.RecoveryPathV1{
		Schema: hostresources.RecoveryPathSchemaV1, ID: "recovery:" + id, Kind: string(kind), EndpointID: "management:" + id,
		PrincipalID: "principal:" + id, VerificationMethod: "provider_console", EvidenceProvider: "generation-provider",
		TargetOperation: "ssh-operation:generation", VerifiedAt: now.Unix(), ExpiresAt: now.Add(10 * time.Minute).Unix(),
		IndependenceClass: "provider_control_plane", VerificationState: "verified", OperationBound: true, Revision: 1,
		SourceRevision: domain.Revision("source-" + id), ConfigurationRevision: domain.Revision("configuration-" + id),
		ProducerRevision: evidenceProducerRevision,
	}
}
