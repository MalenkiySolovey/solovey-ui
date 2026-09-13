package sshmanagement

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	managementregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/management"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
	logger2 "github.com/MalenkiySolovey/solovey-ui/logger"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type workflowProviderFake struct {
	observeCalls int
	onObserve    func()

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
	prepareFailure          bool
	stageFailure            bool
	recoverStageFailure     bool
	recoverStageMismatch    bool
	releaseStageFailure     bool
	validationFailure       bool
	initialReloadFailure    bool
	generation              string
	stageCalls              int
	reloadCalls             int
	restoreCalls            int
	restoreMutations        int
	releaseStageCalls       int
	lastRestoreFence        string
	fences                  []ProviderFenceV1
	reloadRequests          []ReloadRequestV1
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
	f.observeCalls++
	if f.onObserve != nil {
		f.onObserve()
	}
	posture := f.posture
	posture.Endpoints = append([]hostresources.ManagementEndpointV1(nil), f.posture.Endpoints...)
	if f.restoreCalls > 0 && f.restoredPostureMismatch {
		posture.ServiceRevision = domain.Revision("restored-posture-mismatch")
		posture.Service.Digest = posture.ServiceRevision
	}
	sealFixturePosture(&posture, f.now)
	if f.generation != "" {
		posture.ListenerAuthorities[0].Process.StartTime = f.generation
		posture.ListenerAuthorities[0].Seal()
		posture.SemanticRevision = semanticPostureRevision(posture)
	}
	return ObservationV1{Posture: posture, ProviderRevision: f.providerRevision}, nil
}

func (f *workflowProviderFake) PreparePolicy(_ context.Context, request PrepareRequestV1) (PreparedPolicyV1, error) {
	if f.prepareFailure {
		return PreparedPolicyV1{}, domain.NewError("prepare", domain.ReasonUnsupportedDirective)
	}
	representation := "candidate " + domain.Revision(request.Policy)
	digest := domain.Revision([]byte(representation))
	return PreparedPolicyV1{Implementation: f.posture.Binary.Implementation, Format: "fake-representation", Label: "Fake SSH representation",
		Representation: representation, ArtifactDigest: digest}, nil
}

func (f *workflowProviderFake) StageManagedPolicy(_ context.Context, request StageRequestV1) (StageResultV1, error) {
	if err := f.acceptFence(request.Fence); err != nil {
		return StageResultV1{}, err
	}
	f.stageCalls++
	if f.stageFailure {
		return StageResultV1{}, errors.New("stage outcome unavailable")
	}
	if request.Policy.Validate() != nil || !providerDigest(request.ExpectedArtifactDigest) {
		return StageResultV1{}, errors.New("invalid prepared policy")
	}
	f.currentDigest, f.currentPresent = request.ExpectedArtifactDigest, true
	f.currentOwner, f.currentGroup, f.currentMode = "root", "root", "owner_read_write"
	f.posture.ConfigurationRevision = domain.Revision("staged-configuration")
	f.posture.SemanticRevision = semanticPostureRevision(f.posture)
	return StageResultV1{ArtifactDigest: f.currentDigest, EndpointID: request.EndpointID, Prior: f.prior, ProviderRevision: f.providerRevision, ConfigurationRevision: f.posture.ConfigurationRevision}, nil
}

func (f *workflowProviderFake) RecoverCompletedStage(_ context.Context, request RecoverStageRequestV1) (StageResultV1, error) {
	if f.recoverStageFailure || f.stageCalls == 0 || request.EndpointID != request.Fence.EndpointID || request.ExpectedArtifactDigest != f.currentDigest {
		return StageResultV1{}, errors.New("completed stage unavailable")
	}
	endpointID := request.EndpointID
	if f.recoverStageMismatch {
		endpointID = "management:ssh:mismatched"
	}
	return StageResultV1{ArtifactDigest: f.currentDigest, EndpointID: endpointID, Prior: f.prior,
		ProviderRevision: f.providerRevision, ConfigurationRevision: f.posture.ConfigurationRevision}, nil
}

func (f *workflowProviderFake) ReleaseCompletedStage(_ context.Context, request ReleaseStageRequestV1) error {
	f.releaseStageCalls++
	if f.releaseStageFailure || request.EndpointID != request.Fence.EndpointID || request.ExpectedArtifactDigest != request.Fence.CandidateDigest {
		return errors.New("completed stage release unavailable")
	}
	return nil
}

func (f *workflowProviderFake) ValidateManagedPolicy(_ context.Context, request ValidationRequestV1) (ValidationResultV1, error) {
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
	f.reloadRequests = append(f.reloadRequests, request)
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

func (f *workflowProviderFake) RestoreManagedPolicy(_ context.Context, request RestoreRequestV1) (RestoreResultV1, error) {
	if err := f.acceptFence(request.Fence); err != nil {
		return RestoreResultV1{}, err
	}
	f.restoreCalls++
	if f.lastRestoreFence == request.Fence.FencingToken {
		return RestoreResultV1{ArtifactDigest: f.prior.Digest, ConfigurationRevision: domain.Revision("configuration-restored"), ProviderRevision: f.providerRevision}, nil
	}
	if f.restoreFailure {
		return RestoreResultV1{}, errors.New("restore failed")
	}
	if request.ExpectedCurrentArtifactDigest != f.currentDigest {
		return RestoreResultV1{}, errors.New("restore compare-and-swap conflict")
	}
	f.lastRestoreFence = request.Fence.FencingToken
	f.restoreMutations++
	f.currentDigest, f.currentPresent = f.prior.Digest, f.prior.Present
	f.currentOwner, f.currentGroup, f.currentMode = f.prior.Owner, f.prior.Group, f.prior.ModeClass
	f.posture.ConfigurationRevision = domain.Revision("configuration-restored")
	return RestoreResultV1{ArtifactDigest: f.prior.Digest, ConfigurationRevision: f.posture.ConfigurationRevision, ProviderRevision: f.providerRevision}, nil
}

func (f *workflowProviderFake) InspectManagedPolicy(_ context.Context, request InspectRequestV1) (InspectResultV1, error) {
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

func TestCurrentPostureObservesSemanticProviderWithoutPriorWorkflow(t *testing.T) {
	manager, provider, _ := workflowFixture(t)
	provider.posture.Endpoints[0].Port = 2222
	provider.posture.SemanticRevision = semanticPostureRevision(provider.posture)

	if _, err := manager.LatestPosture(context.Background()); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("fresh repository unexpectedly contained a posture: %v", err)
	}
	posture, err := manager.CurrentPosture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(posture.Endpoints) != 1 || posture.Endpoints[0].Port != 2222 || posture.Endpoints[0].ServiceKind != hostresources.ManagementSSH {
		t.Fatalf("semantic provider endpoint was not returned: %#v", posture.Endpoints)
	}
	if _, err := manager.LatestPosture(context.Background()); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("read-only observation unexpectedly persisted state: %v", err)
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
	stored, err := manager.Repository.CandidateByIdempotency(context.Background(), request.IdempotencyKey)
	if err != nil || started.Candidate.EndpointID != request.EndpointID || stored.EndpointID != request.EndpointID {
		t.Fatalf("endpoint binding was not persisted: started=%q stored=%q err=%v", started.Candidate.EndpointID, stored.EndpointID, err)
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
	changed = request
	changed.EndpointID = "management:ssh:observed:ipv4:secondary"
	if _, err := manager.Start(context.Background(), changed); domain.ErrorCode(err) != domain.ReasonIdempotencyConflict {
		t.Fatalf("endpoint-changing idempotency replay returned %v", err)
	}
}

func TestLegacyCandidateEndpointBindingMigratesFromReconnectChallenge(t *testing.T) {
	manager, _, _ := workflowFixture(t)
	request := startFixture(previewFixture(t, manager))
	started, err := manager.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	legacy := started.Candidate
	legacy.EndpointID = ""
	legacy.BindingDigest = domain.BindingDigest(legacy)
	db, _ := manager.Repository.db()
	if err := db.Model(&model.SSHManagementCandidate{}).Where("operation_id = ?", legacy.OperationID).
		Updates(map[string]any{"endpoint_id": "", "binding_digest": legacy.BindingDigest}).Error; err != nil {
		t.Fatal(err)
	}
	confirmed, err := manager.Confirm(context.Background(), ConfirmRequestV1{OperationID: legacy.OperationID,
		ExpectedRevision: legacy.Revision, ProviderEvidenceRef: started.Verifier})
	if err != nil || confirmed.State != domain.StateCommitted || confirmed.EndpointID != request.EndpointID {
		t.Fatalf("legacy candidate was not rebound from its challenge: candidate=%#v err=%v", confirmed, err)
	}
	stored, err := manager.Repository.Candidate(context.Background(), legacy.OperationID)
	if err != nil || stored.EndpointID != confirmed.EndpointID {
		t.Fatalf("rebound endpoint was not persisted: candidate=%#v err=%v", stored, err)
	}
}

func TestLegacyCandidateWithoutBoundChallengeFailsClosed(t *testing.T) {
	manager, provider, _ := workflowFixture(t)
	started, err := manager.Start(context.Background(), startFixture(previewFixture(t, manager)))
	if err != nil {
		t.Fatal(err)
	}
	legacy := started.Candidate
	legacy.EndpointID = ""
	legacy.State = domain.StateStaged
	legacy.BindingDigest = domain.BindingDigest(legacy)
	db, _ := manager.Repository.db()
	if err := db.Model(&model.SSHManagementCandidate{}).Where("operation_id = ?", legacy.OperationID).
		Updates(map[string]any{"endpoint_id": "", "binding_digest": legacy.BindingDigest, "state": string(legacy.State)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("operation_id = ?", legacy.OperationID).Delete(&model.SSHReconnectChallenge{}).Error; err != nil {
		t.Fatal(err)
	}
	provider.restoreCalls = 0
	if err := manager.ReconcileStartup(context.Background()); err == nil {
		t.Fatal("legacy candidate without endpoint evidence was accepted")
	}
	current, err := manager.Repository.Candidate(context.Background(), legacy.OperationID)
	if err != nil || current.State != domain.StateManualRecoveryRequired || provider.restoreCalls != 0 {
		t.Fatalf("candidate=%#v restores=%d err=%v", current, provider.restoreCalls, err)
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
	if len(provider.reloadRequests) != 2 || provider.reloadRequests[0].Recovery || !provider.reloadRequests[1].Recovery ||
		provider.reloadRequests[1].EndpointID != started.Candidate.EndpointID ||
		provider.reloadRequests[1].Fence.ExpectedConfigurationRevision != domain.Revision("configuration-restored") {
		t.Fatalf("recovery reload was not bound to restored configuration: %#v", provider.reloadRequests)
	}
	if err := manager.ReconcileExpired(context.Background()); err != nil || provider.restoreCalls != 1 {
		t.Fatalf("reconcile replay err=%v restores=%d", err, provider.restoreCalls)
	}
}

func TestIdleWatchdogReconciliationIsAStableNoOp(t *testing.T) {
	manager, _, _ := workflowFixture(t)
	for range 4 {
		if err := manager.ReconcileExpired(context.Background()); err != nil {
			t.Fatalf("idle watchdog reconciliation failed: %v", err)
		}
	}
	db, _ := manager.Repository.db()
	var candidates, journals int64
	if err := db.Model(&model.SSHManagementCandidate{}).Count(&candidates).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.SSHManagementJournal{}).Count(&journals).Error; err != nil {
		t.Fatal(err)
	}
	if candidates != 0 || journals != 0 {
		t.Fatalf("idle watchdog mutated workflow state: candidates=%d journals=%d", candidates, journals)
	}
}

func TestWatchdogTickKeepsRealRepositoryFailureVisible(t *testing.T) {
	manager, _, _ := workflowFixture(t)
	db, err := manager.Repository.db()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	before := len(logger2.Entries(100, "debug", "panel", "SSH management watchdog reconciliation failed"))
	manager.watchdogTick(context.Background())
	after := logger2.Entries(100, "debug", "panel", "SSH management watchdog reconciliation failed")
	if len(after) <= before || !strings.Contains(after[0].Message, ":") {
		t.Fatalf("real watchdog failure was not logged with its classification: %#v", after)
	}
}

func TestLegacyReconnectExpiryRebindsEndpointBeforeRollback(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	request := startFixture(previewFixture(t, manager))
	started, err := manager.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	legacy := started.Candidate
	legacy.EndpointID = ""
	legacy.BindingDigest = domain.BindingDigest(legacy)
	db, _ := manager.Repository.db()
	if err := db.Model(&model.SSHManagementCandidate{}).Where("operation_id = ?", legacy.OperationID).
		Updates(map[string]any{"endpoint_id": "", "binding_digest": legacy.BindingDigest}).Error; err != nil {
		t.Fatal(err)
	}
	*now = time.Unix(started.Candidate.ReconnectExpiresAt+1, 0).UTC()
	provider.now = *now
	if err := manager.ReconcileExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, err := manager.Repository.Candidate(context.Background(), legacy.OperationID)
	if err != nil || current.State != domain.StateRolledBack || current.EndpointID != request.EndpointID || provider.restoreCalls != 1 {
		t.Fatalf("candidate=%#v restores=%d err=%v", current, provider.restoreCalls, err)
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

func TestPreviewRejectsUnrepresentablePolicyBeforeStage(t *testing.T) {
	manager, provider, _ := workflowFixture(t)
	provider.prepareFailure = true
	preview, err := manager.Preview(context.Background(), PreviewRequestV1{Policy: domain.DesiredPolicyV1{
		Schema: domain.PolicySchemaV1, PermitRootLogin: domain.RootLoginUnchanged}, Acknowledged: true})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Possible || preview.ConcretePreview != nil || preview.CandidateDigest != "" || provider.stageCalls != 0 ||
		!containsReason(preview.ReasonCodes, domain.ReasonUnsupportedDirective) {
		t.Fatalf("unrepresentable preview = %#v stageCalls=%d", preview, provider.stageCalls)
	}
}

func TestBackendLabelledPreviewRoundTripsWithoutChangingSemanticPolicy(t *testing.T) {
	manager, _, _ := workflowFixture(t)
	preview := previewFixture(t, manager)
	data, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PreviewV1
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ConcretePreview == nil || decoded.ConcretePreview.Implementation != "openssh" ||
		decoded.ConcretePreview.Label != "Fake SSH representation" || decoded.ConcretePreview.Representation == "" {
		t.Fatalf("backend diagnostic preview did not round-trip: %#v", decoded.ConcretePreview)
	}
	if domain.Revision(decoded.Policy) != domain.Revision(preview.Policy) {
		t.Fatalf("semantic policy changed during diagnostic round-trip: %#v != %#v", decoded.Policy, preview.Policy)
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

func TestLegacyCandidateTableAddsEndpointColumnWithoutLosingRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(t, "ssh-management-legacy")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SSHManagementCandidate{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(&model.SSHManagementCandidate{}, "EndpointID"); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(10_000, 0).UTC()
	legacy := domain.CandidateV1{Schema: domain.CandidateSchemaV1, OperationID: "ssh-operation:legacy", IdempotencyKey: "idem:legacy",
		State: domain.StateCommitted, Revision: 1, Policy: domain.DesiredPolicyV1{Schema: domain.PolicySchemaV1, PermitRootLogin: domain.RootLoginUnchanged},
		Preservation:    domain.ManagementPreservationPlanV1{Schema: domain.PreservationSchemaV1, Revision: domain.Revision("legacy-preservation")},
		CandidateDigest: domain.Revision("legacy-candidate"), PostureRevision: domain.Revision("legacy-posture"), EndpointRevision: domain.Revision("legacy-endpoints"),
		RecoveryRevision: domain.Revision("legacy-recovery"), ProviderRevision: domain.Revision("legacy-provider"), BinaryRevision: domain.Revision("legacy-binary"),
		ServiceRevision: domain.Revision("legacy-service"), ConfigurationRevision: domain.Revision("legacy-configuration"), EarliestSafetyExpiry: now.Add(time.Minute).Unix(),
		CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
	legacy.BindingDigest = domain.BindingDigest(legacy)
	row, err := candidateRow(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Omit("EndpointID").Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SSHManagementCandidate{}); err != nil {
		t.Fatalf("legacy endpoint migration failed: %v", err)
	}
	var stored model.SSHManagementCandidate
	if err := db.Where("operation_id = ?", legacy.OperationID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	decoded, err := candidateFromRow(stored)
	if err != nil || decoded.EndpointID != "" || decoded.BindingDigest != legacy.BindingDigest {
		t.Fatalf("legacy candidate row changed during endpoint migration: candidate=%#v err=%v", decoded, err)
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
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(t, "ssh-management")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
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

func testSQLiteDSN(t testing.TB, prefix string) string {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	return "file:" + prefix + "-" + name + "-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "?mode=memory&cache=shared"
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
	sealFixturePosture(&posture, now)
	return posture
}

func sealFixturePosture(posture *domain.SSHPostureV1, now time.Time) {
	if posture == nil || len(posture.Endpoints) == 0 {
		return
	}
	endpoint := posture.Endpoints[0]
	pid, parent, session, root := 784, 1, 784, 0
	authority := domain.SSHListenerAuthorityV1{
		Schema: domain.ListenerAuthoritySchemaV1, EndpointIDs: []string{endpoint.ID}, InstanceID: "sshd.service",
		Socket: hostfacts.ListenerSocketIdentityV1{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: endpoint.Bind, Port: endpoint.Port,
			Inode: "22", Cookie: 42, CoverageFamilies: []hostfacts.Family{hostfacts.FamilyIPv4}},
		Process: hostfacts.ProcessFact{ProviderRevision: "process-evidence/v2", EvidenceRevision: domain.Revision("process"), PID: &pid, ParentPID: &parent,
			SessionID: &session, StartTime: "123", ExeDigest: posture.BinaryRevision, Executable: "/usr/sbin/sshd", ExeDevice: 8, ExeInode: 42,
			UID: &root, GID: &root, ControlGroup: "/system.slice/sshd.service"},
		Service: hostfacts.ServiceFact{SupervisorRevision: domain.Revision("supervisor"), CgroupAvailability: "available", CgroupPolicy: "required",
			CgroupRevision: domain.Revision("cgroup"), MainPID: &pid, ActiveState: "active", SubState: "running", SystemdUnit: "sshd.service",
			FragmentPath: "/usr/lib/systemd/system/sshd.service", FragmentSHA256: domain.Revision("fragment"), ControlGroup: "/system.slice/sshd.service", StartMonotonicUsec: 1},
		BinaryRevision: posture.BinaryRevision, ServiceRevision: posture.ServiceRevision, ConfigurationRevision: posture.ConfigurationRevision,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(),
	}
	authority.Seal()
	posture.ListenerAuthorities = []domain.SSHListenerAuthorityV1{authority}
	posture.SemanticRevision = semanticPostureRevision(*posture)
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

func TestCurrentReadUsesOneObservationWithoutWorkflowHistory(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	for _, history := range []bool{false, true} {
		if history {
			stale := postureFixture(now.Add(-time.Hour))
			if err := manager.Repository.SavePosture(context.Background(), stale, now.Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
		before := provider.observeCalls
		read := manager.CurrentRead(context.Background())
		if !read.Fresh || read.Posture == nil || read.Posture.SemanticRevision != provider.posture.SemanticRevision || provider.observeCalls != before+1 {
			t.Fatalf("current owner observation missing or split: %#v calls=%d", read, provider.observeCalls-before)
		}
		ssh := 0
		for _, endpoint := range read.Endpoints {
			if endpoint.ServiceKind == hostresources.ManagementSSH {
				ssh++
				if endpoint.ID != read.Posture.Endpoints[0].ID || endpoint.ConfigurationRevision != read.Posture.ConfigurationRevision {
					t.Fatal("mixed SSH endpoints")
				}
			}
		}
		if ssh != 1 {
			t.Fatalf("SSH endpoint count %d", ssh)
		}
		latest, err := manager.LatestPosture(context.Background())
		if !history && !errors.Is(err, gorm.ErrRecordNotFound) || history && (err != nil || latest.ObservedAt >= now.Unix()) {
			t.Fatal("current GET changed workflow history")
		}
	}
	manager.Provider = UnavailableProvider{}
	if read := manager.CurrentRead(context.Background()); read.Fresh || len(read.Endpoints) != 1 || read.Endpoints[0].ServiceKind != hostresources.ManagementPanel {
		t.Fatalf("stale SSH authority survived: %#v", read)
	}
}

func TestSSHPreviewValidatesAfterObservationCompletes(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	provider.onObserve = func() { *now = now.Add(time.Second); provider.now = *now }
	preview, err := manager.Preview(context.Background(), PreviewRequestV1{Policy: domain.DesiredPolicyV1{Schema: domain.PolicySchemaV1, PermitRootLogin: domain.RootLoginUnchanged}, Acknowledged: true})
	if err != nil || preview.Posture == nil || !preview.Possible {
		t.Fatalf("post-observation preview rejected current authority: %#v %v", preview, err)
	}
}

func TestCurrentPostureScopeRetainsOneGenerationAndRejectsExpiry(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	ctx := manager.WithCurrentPosture(context.Background())
	first, err := manager.CurrentPosture(ctx)
	if err != nil {
		t.Fatal(err)
	}
	original := first.Endpoints[0].Bind
	first.Endpoints[0].Bind = "corrupted"
	second, err := manager.CurrentPosture(ctx)
	if err != nil || second.Endpoints[0].Bind != original || provider.observeCalls != 1 {
		t.Fatal("refresh split or shared mutable observation")
	}
	*now = now.Add(time.Minute)
	if _, err := manager.CurrentPosture(ctx); err == nil || provider.observeCalls != 1 {
		t.Fatal("expired read silently refreshed")
	}
}

func TestCurrentReadObservesSSHAfterSlowNeutralProviders(t *testing.T) {
	manager, provider, now := workflowFixture(t)
	manager.Endpoints = func(context.Context, time.Time) []hostresources.ManagementEndpointV1 {
		*now = now.Add(time.Minute)
		return []hostresources.ManagementEndpointV1{panelEndpointFixture(*now)}
	}
	provider.onObserve = func() { provider.now = *now; provider.posture = postureFixture(*now) }
	read := manager.CurrentRead(context.Background())
	if !read.Fresh || read.Posture == nil || read.Posture.ListenerAuthorities[0].ObservedAt != now.Unix() || read.Posture.Validate(*now) != nil {
		t.Fatal("neutral provider latency consumed current SSH freshness")
	}
}

func TestSSHPreviewRevisionBindsAuthorityInsteadOfObservationClock(t *testing.T) {
	for _, change := range []string{"clock", "generation", "configuration", "expired_recovery"} {
		t.Run(change, func(t *testing.T) {
			manager, provider, now := workflowFixture(t)
			// A real recovery proof has a fixed verification time and deadline.
			proof := consoleRecoveryFixture(provider.endpoints[0], *now)
			manager.Evidence = func(context.Context, time.Time) managementregistry.EvidenceSnapshot {
				return managementregistry.EvidenceSnapshot{Paths: []hostresources.RecoveryPathV1{proof}}
			}
			before := previewFixture(t, manager)
			*now = now.Add(time.Second)
			provider.now = *now
			provider.posture = postureFixture(*now)
			if change == "generation" {
				provider.generation = "124"
				provider.posture.ListenerAuthorities[0].Seal()
				provider.posture.SemanticRevision = semanticPostureRevision(provider.posture)
			}
			if change == "configuration" {
				provider.posture.ConfigurationRevision = domain.Revision("changed-config")
				provider.posture.ConfigGraph[0].Digest = provider.posture.ConfigurationRevision
				provider.posture.Endpoints[0].ConfigurationRevision = provider.posture.ConfigurationRevision
				sealFixturePosture(&provider.posture, *now)
			}
			if change == "expired_recovery" {
				proof.ExpiresAt = now.Add(-time.Second).Unix()
			}
			after, err := manager.Preview(context.Background(), PreviewRequestV1{Policy: before.Policy, Acknowledged: true})
			if err != nil {
				t.Fatal(err)
			}
			if change == "clock" {
				if !after.Possible || after.Revision != before.Revision || after.EndpointRevision != before.EndpointRevision {
					t.Fatal("clock-only refresh changed semantic approval")
				}
				if _, err := manager.Start(context.Background(), startFixture(before)); err != nil {
					t.Fatalf("unchanged approved authority failed Start: %v", err)
				}
			} else {
				if after.Revision == before.Revision {
					t.Fatal("semantic or safety change kept approval")
				}
				if _, err := manager.Start(context.Background(), startFixture(before)); err == nil {
					t.Fatal("changed authority accepted old approval")
				}
			}
		})
	}
}
