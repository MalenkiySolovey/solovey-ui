package sshmanagement

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	managementregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/management"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type workflowProviderFake struct {
	now                     time.Time
	posture                 domain.SSHPostureV1
	endpoints               []hostresources.ManagementEndpointV1
	providerRevision        string
	currentDigest           string
	currentPresent          bool
	currentOwner            string
	currentGroup            string
	currentMode             string
	prior                   PriorArtifactV1
	restoreFailure          bool
	restoreReloadFailure    bool
	restoredPostureMismatch bool
	stageFailure            bool
	validationFailure       bool
	initialReloadFailure    bool
	stageCalls              int
	reloadCalls             int
	restoreCalls            int
	fences                  []ProviderFenceV1
}

func (f *workflowProviderFake) ProviderID() string { return "fake-ssh-provider" }

func (f *workflowProviderFake) Capabilities(context.Context) domain.CapabilitySetV1 {
	value := domain.CapabilitySetV1{ObservePosture: domain.AvailabilityAvailable, Prepare: domain.AvailabilityAvailable,
		Stage: domain.AvailabilityAvailable, Validate: domain.AvailabilityAvailable, Reload: domain.AvailabilityAvailable,
		Reconnect: domain.AvailabilityAvailable, Rollback: domain.AvailabilityAvailable}
	value.Revision = domain.Revision(value)
	return value
}

func (f *workflowProviderFake) Observe(context.Context) (ObservationV1, error) {
	posture := f.posture
	posture.Endpoints = append([]hostresources.ManagementEndpointV1(nil), f.posture.Endpoints...)
	if f.restoreCalls > 0 && f.restoredPostureMismatch {
		posture.ServiceRevision = domain.Revision("restored-posture-mismatch")
		posture.Service.Digest = posture.ServiceRevision
		posture.SemanticRevision = semanticPostureRevision(posture)
	}
	return ObservationV1{Posture: posture, ProviderRevision: f.providerRevision}, nil
}

func (f *workflowProviderFake) StageManagedDropIn(_ context.Context, request StageRequestV1) (StageResultV1, error) {
	if err := f.acceptFence(request.Fence); err != nil {
		return StageResultV1{}, err
	}
	f.stageCalls++
	if f.stageFailure {
		return StageResultV1{}, errors.New("stage outcome unavailable")
	}
	f.currentDigest, f.currentPresent = domain.Revision(request.ManagedContent), true
	f.currentOwner, f.currentGroup, f.currentMode = "root", "root", "owner_read_write"
	f.posture.ConfigurationRevision = domain.Revision("staged-configuration")
	f.posture.SemanticRevision = semanticPostureRevision(f.posture)
	return StageResultV1{ArtifactDigest: f.currentDigest, Prior: f.prior, ProviderRevision: f.providerRevision, ConfigurationRevision: f.posture.ConfigurationRevision}, nil
}

func (f *workflowProviderFake) ValidateManagedDropIn(_ context.Context, request ValidationRequestV1) (ValidationResultV1, error) {
	if err := f.acceptFence(request.Fence); err != nil {
		return ValidationResultV1{}, err
	}
	if f.validationFailure {
		return ValidationResultV1{ProviderRevision: f.providerRevision}, nil
	}
	return ValidationResultV1{SyntaxValid: request.ArtifactDigest == f.currentDigest, EffectiveValid: true,
		EffectiveRevision: domain.Revision("effective"), ProviderRevision: f.providerRevision}, nil
}

func (f *workflowProviderFake) ReloadSelectedService(_ context.Context, request ReloadRequestV1) (ReloadResultV1, error) {
	if err := f.acceptFence(request.Fence); err != nil {
		return ReloadResultV1{}, err
	}
	f.reloadCalls++
	if f.restoreCalls == 0 && f.initialReloadFailure {
		return ReloadResultV1{}, errors.New("initial reload failed")
	}
	if f.restoreCalls > 0 && f.restoreReloadFailure {
		return ReloadResultV1{}, errors.New("restore reload failed")
	}
	if f.restoreCalls > 0 {
		f.posture.ServiceRevision = domain.Revision("service-restored")
		f.posture.ConfigurationRevision = domain.Revision("configuration-restored")
	} else {
		f.posture.ServiceRevision = domain.Revision("service-reloaded")
		f.posture.ConfigurationRevision = domain.Revision("configuration-reloaded")
	}
	f.posture.Service.Digest = f.posture.ServiceRevision
	f.posture.ObservedAt = f.now.Unix()
	f.posture.ExpiresAt = f.now.Add(5 * time.Minute).Unix()
	for index := range f.posture.Endpoints {
		if f.posture.Endpoints[index].ServiceKind == hostresources.ManagementSSH {
			f.posture.Endpoints[index].ConfigurationRevision = f.posture.ConfigurationRevision
			f.posture.Endpoints[index].SemanticRevision = f.posture.ConfigurationRevision
			f.posture.Endpoints[index].ObservedAt = f.now.Unix()
			f.posture.Endpoints[index].ExpiresAt = f.now.Add(5 * time.Minute).Unix()
		}
	}
	f.posture.SemanticRevision = semanticPostureRevision(f.posture)
	for index := range f.endpoints {
		if f.endpoints[index].ServiceKind == hostresources.ManagementSSH {
			f.endpoints[index].ConfigurationRevision = f.posture.ConfigurationRevision
			f.endpoints[index].SemanticRevision = f.posture.ConfigurationRevision
		}
	}
	return ReloadResultV1{ServiceRevision: f.posture.ServiceRevision, ConfigurationRevision: f.posture.ConfigurationRevision, ProviderRevision: f.providerRevision}, nil
}

func (f *workflowProviderFake) VerifyReconnect(_ context.Context, proof ReconnectProofV1) (ReconnectResultV1, error) {
	if err := f.acceptFence(proof.Fence); err != nil {
		return ReconnectResultV1{}, err
	}
	return ReconnectResultV1{Verified: true, Independent: true, FreshSession: true, OperationBound: true,
		EndpointID: proof.EndpointID, PrincipalID: proof.PrincipalID, AuthenticationClass: proof.AuthenticationClass,
		EvidenceRevision: domain.Revision(struct{ Operation, Marker string }{proof.Fence.OperationID, proof.MarkerDigest})}, nil
}

func (f *workflowProviderFake) RestoreManagedDropIn(_ context.Context, request RestoreRequestV1) (RestoreResultV1, error) {
	if err := f.acceptFence(request.Fence); err != nil {
		return RestoreResultV1{}, err
	}
	f.restoreCalls++
	if f.restoreFailure {
		return RestoreResultV1{}, errors.New("restore failed")
	}
	if request.ExpectedCurrentArtifactDigest != f.currentDigest {
		return RestoreResultV1{}, errors.New("restore compare-and-swap conflict")
	}
	f.currentDigest, f.currentPresent = f.prior.Digest, f.prior.Present
	f.currentOwner, f.currentGroup, f.currentMode = f.prior.Owner, f.prior.Group, f.prior.ModeClass
	f.posture.ConfigurationRevision = domain.Revision("configuration-restored")
	return RestoreResultV1{ArtifactDigest: f.prior.Digest, ConfigurationRevision: f.posture.ConfigurationRevision, ProviderRevision: f.providerRevision}, nil
}

func (f *workflowProviderFake) InspectManagedDropIn(_ context.Context, request InspectRequestV1) (InspectResultV1, error) {
	if err := f.acceptFence(request.Fence); err != nil {
		return InspectResultV1{}, err
	}
	return InspectResultV1{Present: f.currentPresent, ArtifactDigest: f.currentDigest, Owner: f.currentOwner, Group: f.currentGroup,
		ModeClass: f.currentMode, ConfigurationRevision: f.posture.ConfigurationRevision}, nil
}

func (f *workflowProviderFake) acceptFence(fence ProviderFenceV1) error {
	if err := fence.Validate(f.now); err != nil {
		return err
	}
	f.fences = append(f.fences, fence)
	return nil
}

func TestWorkflowCommitsOnlyAfterFreshOperationBoundReconnect(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	preview := previewFixture(t, manager)
	request := startFixture(preview)
	started, err := manager.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if started.Candidate.State != domain.StateReconnectRequired || started.Verifier == "" || provider.stageCalls != 1 || provider.reloadCalls != 1 {
		t.Fatalf("started=%#v stage=%d reload=%d", started.Candidate, provider.stageCalls, provider.reloadCalls)
	}
	confirmed, err := manager.Confirm(context.Background(), ConfirmRequestV1{OperationID: started.Candidate.OperationID, ExpectedRevision: started.Candidate.Revision, ProviderEvidenceRef: started.Verifier})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if confirmed.State != domain.StateCommitted || confirmed.RollbackAttempts != 0 || !confirmed.Preservation.Safe {
		t.Fatalf("confirmed=%#v", confirmed)
	}
	paths, err := manager.RecoveryPaths(context.Background(), *now)
	if err != nil || len(paths) != 1 || !paths[0].OperationBound || !paths[0].SingleUse || paths[0].TargetOperation != confirmed.OperationID {
		t.Fatalf("paths=%#v err=%v", paths, err)
	}
	if _, err := manager.Confirm(context.Background(), ConfirmRequestV1{OperationID: started.Candidate.OperationID,
		ExpectedRevision: started.Candidate.Revision, ProviderEvidenceRef: started.Verifier}); err == nil {
		t.Fatal("reconnect challenge replay was accepted")
	}
}

func TestEveryProviderMutationRequestIsFencedAndBounded(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	started, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Confirm(context.Background(), ConfirmRequestV1{OperationID: started.Candidate.OperationID,
		ExpectedRevision: started.Candidate.Revision, ProviderEvidenceRef: started.Verifier}); err != nil {
		t.Fatal(err)
	}
	if len(provider.fences) < 5 {
		t.Fatalf("provider calls without complete fence coverage: %d", len(provider.fences))
	}
	var previous uint64
	for index, fence := range provider.fences {
		if err := fence.Validate(*now); err != nil {
			t.Fatalf("fence %d invalid: %v", index, err)
		}
		if fence.OperationID != started.Candidate.OperationID || fence.CandidateRevision < previous || fence.DeadlineAt > now.Add(MaxProviderRequestDuration).Unix() {
			t.Fatalf("fence %d is stale or unbound: %#v", index, fence)
		}
		previous = fence.CandidateRevision
	}
}

func TestCandidateIdempotencyReplaysOnlyTheSamePolicy(t *testing.T) {
	manager, provider, _ := workflowFixture(t)
	preview := previewFixture(t, manager)
	request := startFixture(preview)
	started, err := manager.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := manager.Start(context.Background(), request)
	if err != nil || replayed.Candidate.OperationID != started.Candidate.OperationID || provider.stageCalls != 1 {
		t.Fatalf("replayed=%#v stageCalls=%d err=%v", replayed, provider.stageCalls, err)
	}
	changed := request
	changed.Policy.PermitRootLogin = domain.RootLoginNo
	if _, err := manager.Start(context.Background(), changed); domain.ErrorCode(err) != domain.ReasonIdempotencyConflict {
		t.Fatalf("conflicting idempotency key returned %v", err)
	}
}

func TestPreviewIsZeroWriteAndPasswordDisableFailsWithoutFreshPubkey(t *testing.T) {
	manager, _, _ := workflowFixture(t)
	value := false
	preview, err := manager.Preview(context.Background(), PreviewRequestV1{Policy: domain.DesiredPolicyV1{Schema: domain.PolicySchemaV1,
		PermitRootLogin: domain.RootLoginUnchanged, PasswordAuthentication: &value}, Acknowledged: true})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Possible || !containsReason(preview.ReasonCodes, domain.ReasonFreshPubkeyMissing) {
		t.Fatalf("preview=%#v", preview)
	}
	var candidates, snapshots int64
	db, _ := manager.Repository.db()
	_ = db.Model(&model.SSHManagementCandidate{}).Count(&candidates).Error
	_ = db.Model(&model.SSHPostureSnapshot{}).Count(&snapshots).Error
	if candidates != 0 || snapshots != 0 {
		t.Fatalf("preview wrote candidates=%d snapshots=%d", candidates, snapshots)
	}
}

func TestReconnectExpiryRollsBackExactlyOnce(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	preview := previewFixture(t, manager)
	started, err := manager.Start(context.Background(), startFixture(preview))
	if err != nil {
		t.Fatal(err)
	}
	*now = time.Unix(started.Candidate.ReconnectExpiresAt+1, 0).UTC()
	provider.now = *now
	if err := manager.ReconcileExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, err := manager.Candidate(context.Background(), started.Candidate.OperationID)
	if err != nil || current.State != domain.StateRolledBack || current.RollbackAttempts != 1 || provider.restoreCalls != 1 || provider.reloadCalls != 2 {
		t.Fatalf("candidate=%#v restores=%d reloads=%d err=%v", current, provider.restoreCalls, provider.reloadCalls, err)
	}
	if err := manager.ReconcileExpired(context.Background()); err != nil || provider.restoreCalls != 1 {
		t.Fatalf("reconcile replay err=%v restores=%d", err, provider.restoreCalls)
	}
}

func TestStageAmbiguityStopsInManualRecovery(t *testing.T) {
	manager, provider, _ := workflowFixture(t)
	provider.stageFailure = true
	if _, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager))); err == nil {
		t.Fatal("ambiguous stage failure was accepted")
	}
	candidate, err := manager.Repository.CandidateByIdempotency(context.Background(), "idem:test-one")
	if err != nil || candidate.State != domain.StateManualRecoveryRequired || provider.restoreCalls != 0 {
		t.Fatalf("candidate=%#v restores=%d err=%v", candidate, provider.restoreCalls, err)
	}
}

func TestValidationAndReloadFailuresRestoreExactPriorState(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*workflowProviderFake)
	}{
		{name: "validation", mutate: func(provider *workflowProviderFake) { provider.validationFailure = true }},
		{name: "reload", mutate: func(provider *workflowProviderFake) { provider.initialReloadFailure = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager, provider, _ := workflowFixture(t)
			test.mutate(provider)
			if _, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager))); err == nil {
				t.Fatal("failed candidate was accepted")
			}
			candidate, err := manager.Repository.CandidateByIdempotency(context.Background(), "idem:test-one")
			if err != nil || candidate.State != domain.StateRolledBack || provider.restoreCalls != 1 || provider.currentDigest != provider.prior.Digest {
				t.Fatalf("candidate=%#v provider=%#v err=%v", candidate, provider, err)
			}
		})
	}
}

func TestForeignArtifactDriftRequiresManualRecovery(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	started, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager)))
	if err != nil {
		t.Fatal(err)
	}
	provider.currentDigest = domain.Revision("foreign-newer-artifact")
	*now = time.Unix(started.Candidate.ReconnectExpiresAt+1, 0).UTC()
	provider.now = *now
	if err := manager.ReconcileExpired(context.Background()); err == nil {
		t.Fatal("foreign artifact drift was overwritten")
	}
	candidate, err := manager.Candidate(context.Background(), started.Candidate.OperationID)
	if err != nil || candidate.State != domain.StateManualRecoveryRequired || provider.currentDigest == provider.prior.Digest {
		t.Fatalf("candidate=%#v current=%s err=%v", candidate, provider.currentDigest, err)
	}
}

func TestRestoreReloadFailureRequiresManualRecovery(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	started, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager)))
	if err != nil {
		t.Fatal(err)
	}
	provider.restoreReloadFailure = true
	*now = time.Unix(started.Candidate.ReconnectExpiresAt+1, 0).UTC()
	provider.now = *now
	if err := manager.ReconcileExpired(context.Background()); err == nil {
		t.Fatal("restored configuration reload failure was accepted")
	}
	current, err := manager.Candidate(context.Background(), started.Candidate.OperationID)
	if err != nil || current.State != domain.StateManualRecoveryRequired || current.RollbackAttempts != 1 || provider.reloadCalls != 2 {
		t.Fatalf("candidate=%#v reloads=%d err=%v", current, provider.reloadCalls, err)
	}
}

func TestRestoredPostureMismatchRequiresManualRecovery(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	started, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager)))
	if err != nil {
		t.Fatal(err)
	}
	provider.restoredPostureMismatch = true
	*now = time.Unix(started.Candidate.ReconnectExpiresAt+1, 0).UTC()
	provider.now = *now
	if err := manager.ReconcileExpired(context.Background()); err == nil {
		t.Fatal("restored posture mismatch was accepted")
	}
	current, err := manager.Candidate(context.Background(), started.Candidate.OperationID)
	if err != nil || current.State != domain.StateManualRecoveryRequired || current.RollbackAttempts != 1 {
		t.Fatalf("candidate=%#v err=%v", current, err)
	}
}

func TestStartupReconciliationRollsBackInterruptedCandidateOnce(t *testing.T) {
	manager, provider, _ := workflowFixture(t)
	started, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager)))
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewManager(manager.Repository, provider)
	restarted.Now, restarted.Endpoints, restarted.Evidence = manager.Now, manager.Endpoints, manager.Evidence
	if err := restarted.ReconcileStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, err := restarted.Candidate(context.Background(), started.Candidate.OperationID)
	if err != nil || current.State != domain.StateRolledBack || provider.restoreCalls != 1 || provider.reloadCalls != 2 {
		t.Fatalf("candidate=%#v restores=%d reloads=%d err=%v", current, provider.restoreCalls, provider.reloadCalls, err)
	}
	if err := restarted.ReconcileStartup(context.Background()); err != nil || provider.restoreCalls != 1 || provider.reloadCalls != 2 {
		t.Fatalf("replayed startup reconciliation err=%v restores=%d reloads=%d", err, provider.restoreCalls, provider.reloadCalls)
	}
}

func TestRollbackFailureRequiresManualRecovery(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	started, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager)))
	if err != nil {
		t.Fatal(err)
	}
	provider.restoreFailure = true
	*now = time.Unix(started.Candidate.ReconnectExpiresAt+1, 0).UTC()
	provider.now = *now
	if err := manager.ReconcileExpired(context.Background()); err == nil {
		t.Fatal("rollback failure was accepted")
	}
	current, err := manager.Candidate(context.Background(), started.Candidate.OperationID)
	if err != nil || current.State != domain.StateManualRecoveryRequired || current.RollbackAttempts != 1 || provider.restoreCalls != 1 {
		t.Fatalf("candidate=%#v restores=%d err=%v", current, provider.restoreCalls, err)
	}
}

func TestRestoreDistrustsCommittedStateAndBlocksDropData(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	started, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager)))
	if err != nil {
		t.Fatal(err)
	}
	committed, err := manager.Confirm(context.Background(), ConfirmRequestV1{OperationID: started.Candidate.OperationID,
		ExpectedRevision: started.Candidate.Revision, ProviderEvidenceRef: started.Verifier})
	if err != nil || committed.ReconciledAt == 0 {
		t.Fatalf("committed=%#v err=%v", committed, err)
	}
	mutations := provider.stageCalls + provider.reloadCalls + provider.restoreCalls
	if err := manager.Repository.MarkRestoredUntrusted(context.Background(), *now); err != nil {
		t.Fatal(err)
	}
	restored, err := manager.Candidate(context.Background(), committed.OperationID)
	if err != nil || !restored.RestoredUntrusted || restored.ReconciledAt != 0 || restored.State != domain.StateCommitted {
		t.Fatalf("restored=%#v err=%v", restored, err)
	}
	if got := provider.stageCalls + provider.reloadCalls + provider.restoreCalls; got != mutations {
		t.Fatalf("restore hook mutated provider: before=%d after=%d", mutations, got)
	}
	if err := manager.Repository.DropData(context.Background()); err == nil {
		t.Fatal("drop data accepted untrusted restored state")
	}
}

func TestDropDataRequiresTerminalReconciledState(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	started, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager)))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Repository.DropData(context.Background()); err == nil {
		t.Fatal("drop data accepted active candidate")
	}
	*now = time.Unix(started.Candidate.ReconnectExpiresAt+1, 0).UTC()
	provider.now = *now
	if err := manager.ReconcileExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := manager.Candidate(context.Background(), started.Candidate.OperationID)
	if err != nil || rolledBack.State != domain.StateRolledBack || rolledBack.ReconciledAt == 0 {
		t.Fatalf("candidate=%#v err=%v", rolledBack, err)
	}
	if err := manager.Repository.DropData(context.Background()); err != nil {
		t.Fatalf("drop reconciled terminal data: %v", err)
	}
	var count int64
	db, _ := manager.Repository.db()
	if err := db.Model(&model.SSHManagementCandidate{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("remaining candidates=%d err=%v", count, err)
	}
}

func TestCandidateTransitionAndJournalAreAtomic(t *testing.T) {
	manager, _, now := workflowFixture(t)
	candidate := domain.CandidateV1{Schema: domain.CandidateSchemaV1, OperationID: "ssh-operation:atomic", IdempotencyKey: "idem:atomic",
		State: domain.StateDraft, Revision: 1, Policy: domain.DesiredPolicyV1{Schema: domain.PolicySchemaV1, PermitRootLogin: domain.RootLoginUnchanged},
		CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
	candidate.BindingDigest = domain.BindingDigest(candidate)
	if err := manager.Repository.CreateCandidateWithJournal(context.Background(), candidate, "draft_created", "", *now); err != nil {
		t.Fatal(err)
	}
	db, _ := manager.Repository.db()
	conflict := model.SSHManagementJournal{OperationID: candidate.OperationID, Sequence: 2, State: string(domain.StatePreflighted),
		Event: "conflict", Revision: domain.Revision("conflict"), CreatedAt: now.Unix()}
	if err := db.Create(&conflict).Error; err != nil {
		t.Fatal(err)
	}
	next := candidate
	next.State, next.Revision = domain.StatePreflighted, 2
	if err := manager.Repository.UpdateCandidateCASWithJournal(context.Background(), next, candidate.Revision, candidate.State, "preflight_completed", "", *now); err == nil {
		t.Fatal("journal conflict was accepted")
	}
	stored, err := manager.Repository.Candidate(context.Background(), candidate.OperationID)
	if err != nil || stored.State != domain.StateDraft || stored.Revision != 1 {
		t.Fatalf("transition was not rolled back: candidate=%#v err=%v", stored, err)
	}
}

func TestCommitCandidateEvidenceAndChallengeAreAtomic(t *testing.T) {
	manager, _, now := workflowFixture(t)
	started, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager)))
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := manager.Repository.Challenge(context.Background(), started.Candidate.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	next := started.Candidate
	next.State, next.Revision, next.ReconciledAt = domain.StateCommitted, next.Revision+1, now.Unix()
	conflict := model.SSHManagementJournal{OperationID: next.OperationID, Sequence: next.Revision, State: string(next.State),
		Event: "conflict", Revision: domain.Revision("commit-conflict"), CreatedAt: now.Unix()}
	db, _ := manager.Repository.db()
	if err := db.Create(&conflict).Error; err != nil {
		t.Fatal(err)
	}
	path := consoleRecoveryFixture(providerEndpoint(t, manager), *now)
	path.ID = "recovery:atomic-commit"
	if err := manager.Repository.CommitCandidateCASWithJournalAndEvidence(context.Background(), next, started.Candidate.Revision,
		started.Candidate.State, challenge.Revision, path, *now); err == nil {
		t.Fatal("commit journal conflict was accepted")
	}
	stored, err := manager.Repository.Candidate(context.Background(), started.Candidate.OperationID)
	if err != nil || stored.State != domain.StateReconnectRequired || stored.Revision != started.Candidate.Revision {
		t.Fatalf("candidate transaction leaked: candidate=%#v err=%v", stored, err)
	}
	storedChallenge, err := manager.Repository.Challenge(context.Background(), started.Candidate.OperationID)
	if err != nil || storedChallenge.ConsumedAt != 0 || storedChallenge.Revision != challenge.Revision {
		t.Fatalf("challenge transaction leaked: challenge=%#v err=%v", storedChallenge, err)
	}
	var evidenceCount int64
	if err := db.Model(&model.SSHRecoveryEvidence{}).Where("id = ?", path.ID).Count(&evidenceCount).Error; err != nil || evidenceCount != 0 {
		t.Fatalf("evidence transaction leaked: count=%d err=%v", evidenceCount, err)
	}
}

func providerEndpoint(t *testing.T, manager *Manager) hostresources.ManagementEndpointV1 {
	t.Helper()
	for _, endpoint := range manager.EndpointSnapshot(context.Background()) {
		if endpoint.ServiceKind == hostresources.ManagementPanel {
			return endpoint
		}
	}
	t.Fatal("panel endpoint missing")
	return hostresources.ManagementEndpointV1{}
}

func workflowFixture(t *testing.T) (*Manager, *workflowProviderFake, *time.Time) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SSHPostureSnapshot{}, &model.SSHManagementCandidate{}, &model.SSHManagedArtifactCheckpoint{},
		&model.SSHReconnectChallenge{}, &model.SSHRecoveryEvidence{}, &model.SSHManagementJournal{}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(10_000, 0).UTC()
	posture := postureFixture(now)
	panel := panelEndpointFixture(now)
	provider := &workflowProviderFake{now: now, posture: posture, endpoints: []hostresources.ManagementEndpointV1{panel, posture.Endpoints[0]}, providerRevision: domain.Revision("provider-v1"),
		prior:         PriorArtifactV1{Present: false, Content: nil, Owner: "root", Group: "root", ModeClass: "owner_read_write", Digest: domain.Revision([]byte{})},
		currentDigest: domain.Revision([]byte{}), currentOwner: "root", currentGroup: "root", currentMode: "owner_read_write"}
	manager := NewManager(NewRepository(db), provider)
	manager.Now = func() time.Time { return now }
	manager.Random = bytes.NewReader(bytes.Repeat([]byte{0x42}, 128))
	manager.Endpoints = func(context.Context, time.Time) []hostresources.ManagementEndpointV1 {
		return append([]hostresources.ManagementEndpointV1(nil), provider.endpoints...)
	}
	manager.Evidence = func(context.Context, time.Time) managementregistry.EvidenceSnapshot {
		return managementregistry.EvidenceSnapshot{Paths: []hostresources.RecoveryPathV1{consoleRecoveryFixture(provider.endpoints[0], now)}, GeneratedAt: now.Unix()}
	}
	return manager, provider, &now
}

func postureFixture(now time.Time) domain.SSHPostureV1 {
	configuration := domain.Revision("ssh-config")
	ssh := hostresources.ManagementEndpointV1{Schema: hostresources.ManagementEndpointSchemaV1, ID: "management:ssh:observed:ipv4",
		Network: hostresources.NetworkTCP, Family: hostresources.AddressFamilyIPv4, Bind: "192.0.2.10", Port: 22,
		ServiceKind: hostresources.ManagementSSH, Exposure: hostresources.EndpointIntentPublic, Owner: "system", Purpose: "ssh_administrative_access",
		RecoveryPolicy: "fresh_independent_path_required", Source: "fixture", ObservedListener: true, ConfidenceBP: 10000,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(5 * time.Minute).Unix(), ConfigurationRevision: configuration, SemanticRevision: configuration}
	posture := domain.SSHPostureV1{Schema: domain.PostureSchemaV1,
		Binary:        domain.BinaryIdentityV1{Implementation: "openssh", VersionClass: "portable_9", Digest: domain.Revision("binary"), Selected: true},
		Service:       domain.ServiceIdentityV1{Manager: "systemd", UnitID: "sshd.service", State: "active", Digest: domain.Revision("service")},
		ConfigGraph:   []domain.ConfigNodeV1{{ID: "main", Kind: "main", Order: 0, Depth: 0, Digest: configuration, Owner: "root", ModeClass: "owner_read_write"}},
		MatchContexts: []domain.MatchContextV1{{ID: "global", ConditionClass: "global", EffectiveHash: domain.Revision("effective-global"), Known: true}},
		Endpoints:     []hostresources.ManagementEndpointV1{ssh}, Authentication: domain.AuthenticationPostureV1{PasswordAuthentication: "yes", KbdInteractiveAuthentication: "yes",
			PermitRootLogin: "prohibit-password", PubkeyAuthentication: "yes", AuthenticationMethods: []string{"publickey"}, MaxAuthTries: 6,
			LoginGraceTimeSeconds: 120, MaxStartupsClass: "bounded_default"},
		Forwarding:     domain.ForwardingPostureV1{AllowAgentForwarding: "yes", AllowTCPForwarding: "yes", GatewayPorts: "no", PermitTunnel: "no", X11Forwarding: "yes"},
		AuthorizedKeys: domain.AuthorizedKeysPostureV1{StrictModes: "yes", PathTemplateCount: 1, PathTemplateRevision: domain.Revision("authorized-key-templates")},
		HostKeys:       []domain.HostKeyPostureV1{{Type: "ed25519", Fingerprint: domain.Revision("host-key"), Count: 1, Owner: "root", ModeClass: "owner_read"}},
		Capabilities:   (&workflowProviderFake{}).Capabilities(context.Background()), ObservedAt: now.Unix(), ExpiresAt: now.Add(5 * time.Minute).Unix(),
		BinaryRevision: domain.Revision("binary"), ServiceRevision: domain.Revision("service"), ConfigurationRevision: configuration}
	posture.SemanticRevision = semanticPostureRevision(posture)
	return posture
}

func panelEndpointFixture(now time.Time) hostresources.ManagementEndpointV1 {
	panelRevision := domain.Revision("panel-config")
	return hostresources.ManagementEndpointV1{Schema: hostresources.ManagementEndpointSchemaV1, ID: "management:panel:configured:ipv4",
		Network: hostresources.NetworkTCP, Family: hostresources.AddressFamilyIPv4, Bind: "192.0.2.10", Port: 443,
		ServiceKind: hostresources.ManagementPanel, Exposure: hostresources.EndpointIntentPublic, Owner: "panel", Purpose: "administrative_access",
		RecoveryPolicy: "fresh_independent_path_required", Source: "fixture", ConfiguredIntent: true, ConfidenceBP: 10000,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(5 * time.Minute).Unix(), ConfigurationRevision: panelRevision, SemanticRevision: panelRevision}
}

func semanticPostureRevision(posture domain.SSHPostureV1) string {
	return domain.PostureSemanticRevision(posture)
}

func consoleRecoveryFixture(endpoint hostresources.ManagementEndpointV1, now time.Time) hostresources.RecoveryPathV1 {
	return hostresources.RecoveryPathV1{Schema: hostresources.RecoveryPathSchemaV1, ID: "recovery:provider-console", Kind: string(endpoint.ServiceKind),
		EndpointID: endpoint.ID, PrincipalID: "principal:console", VerificationMethod: "provider_console", EvidenceProvider: "fixture-console",
		TargetOperation: "ssh-preflight", VerifiedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(10 * time.Minute).Unix(),
		IndependenceClass: "provider_control_plane", VerificationState: "verified", OperationBound: true, Revision: 1,
		SourceRevision: domain.Revision("console-source"), ConfigurationRevision: endpoint.ConfigurationRevision, ProducerRevision: domain.Revision("fixture-producer")}
}

func previewFixture(t *testing.T, manager *Manager) PreviewV1 {
	t.Helper()
	tries := uint16(4)
	preview, err := manager.Preview(context.Background(), PreviewRequestV1{Policy: domain.DesiredPolicyV1{Schema: domain.PolicySchemaV1,
		PermitRootLogin: domain.RootLoginUnchanged, MaxAuthTries: &tries}, Acknowledged: true})
	if err != nil || !preview.Possible || preview.Posture == nil {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	return preview
}

func startFixture(preview PreviewV1) StartRequestV1 {
	return StartRequestV1{Policy: preview.Policy, IdempotencyKey: "idem:test-one", ExpectedPreviewRevision: preview.Revision,
		ExpectedPostureRevision: preview.PostureRevision, ExpectedEndpointRevision: preview.EndpointRevision,
		ExpectedRecoveryRevision: preview.RecoveryRevision, ExpectedProviderRevision: preview.ProviderRevision,
		EndpointID: "management:ssh:observed:ipv4", PrincipalID: "principal:administrator", AuthenticationClass: "publickey", Acknowledged: true}
}

func containsReason(values []domain.ReasonCode, expected domain.ReasonCode) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
