package nativefallback

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type workflowCoreFake struct {
	*fakeCoreReader
	checkpoint        coreinboundcontrol.CheckpointStatusV1
	beforeEffective   string
	observation       coreinboundcontrol.RuntimeInboundObservationV1
	events            *[]string
	applyCalls        int
	restoreCalls      int
	verifyCalls       int
	releaseCalls      int
	failApply         bool
	failPrepare       bool
	failVerify        bool
	failRestore       bool
	badRestoreVerify  bool
	missingCheckpoint bool
	cancelAfterCommit context.CancelFunc
	afterApply        func()
}

func (core *workflowCoreFake) PrepareCheckpoint(_ context.Context, request coreinboundcontrol.PrepareCheckpointRequestV1) (coreinboundcontrol.CheckpointPreparationV1, error) {
	core.record("core:prepare")
	if core.failPrepare {
		return coreinboundcontrol.CheckpointPreparationV1{}, errors.New("prepare")
	}
	core.checkpoint = coreinboundcontrol.CheckpointStatusV1{
		CheckpointID: "checkpoint-one", State: coreinboundcontrol.CheckpointStatePrepared, PreviewDigest: request.Preview.Digest,
		IntegrityDigest: strings.Repeat("b", 64), BeforeConfigurationRevision: request.Preview.BeforeConfigurationRevision,
		ExpectedAfterRevision: request.Preview.ExpectedAfterRevision, CurrentConfigurationRevision: request.Preview.BeforeConfigurationRevision,
		CurrentEffectiveRevision: core.snapshot.Effective.Revision, DetachedReleaseProof: strings.Repeat("e", 64),
		UncommittedReleaseProof: strings.Repeat("c", 64),
	}
	return coreinboundcontrol.CheckpointPreparationV1{
		Schema: coreinboundcontrol.FallbackCheckpointSchemaV1, CheckpointID: core.checkpoint.CheckpointID,
		PreviewDigest: core.checkpoint.PreviewDigest, IntegrityDigest: core.checkpoint.IntegrityDigest,
		UncommittedReleaseProof: strings.Repeat("c", 64), ExpiresAt: request.Preview.ExpiresAt,
	}, nil
}

func (core *workflowCoreFake) InspectCheckpoint(context.Context, coreinboundcontrol.InspectCheckpointRequestV1) (coreinboundcontrol.CheckpointStatusV1, error) {
	core.record("core:inspect")
	if core.missingCheckpoint {
		return coreinboundcontrol.CheckpointStatusV1{}, errors.New("missing")
	}
	core.checkpoint.CurrentConfigurationRevision = core.snapshot.ConfigurationRevision
	core.checkpoint.CurrentEffectiveRevision = core.snapshot.Effective.Revision
	return core.checkpoint, nil
}

func (core *workflowCoreFake) FindCheckpoint(_ context.Context, request coreinboundcontrol.FindCheckpointRequestV1) (coreinboundcontrol.CheckpointStatusV1, error) {
	core.record("core:find")
	if core.missingCheckpoint || core.checkpoint.CheckpointID == "" || core.checkpoint.PreviewDigest != request.PreviewDigest {
		return coreinboundcontrol.CheckpointStatusV1{}, &coreinboundcontrol.AdapterError{Code: coreinboundcontrol.ErrorCheckpointMissing}
	}
	core.checkpoint.CurrentConfigurationRevision = core.snapshot.ConfigurationRevision
	core.checkpoint.CurrentEffectiveRevision = core.snapshot.Effective.Revision
	return core.checkpoint, nil
}

func (core *workflowCoreFake) ApplyFallbackPatch(context.Context, coreinboundcontrol.ApplyFallbackPatchRequestV1) (coreinboundcontrol.FallbackMutationResultV1, error) {
	core.record("core:apply")
	core.applyCalls++
	if core.failApply {
		return coreinboundcontrol.FallbackMutationResultV1{}, errors.New("apply")
	}
	core.snapshot.ConfigurationRevision = core.checkpoint.ExpectedAfterRevision
	core.snapshot.Effective.Revision = strings.Repeat("7", 64)
	core.checkpoint.State = coreinboundcontrol.CheckpointStateRuntimeApplied
	core.checkpoint.CurrentConfigurationRevision = core.snapshot.ConfigurationRevision
	core.checkpoint.CurrentEffectiveRevision = core.snapshot.Effective.Revision
	if core.afterApply != nil {
		core.afterApply()
	}
	if core.cancelAfterCommit != nil {
		core.cancelAfterCommit()
		return coreinboundcontrol.FallbackMutationResultV1{}, &coreinboundcontrol.AdapterError{Code: coreinboundcontrol.ErrorAmbiguousResult}
	}
	return coreinboundcontrol.FallbackMutationResultV1{
		Schema: coreinboundcontrol.FallbackMutationSchemaV1, CheckpointID: core.checkpoint.CheckpointID,
		InboundDatabaseID: core.snapshot.InboundDatabaseID, BeforeConfigurationRevision: strings.Repeat("5", 64),
		AfterConfigurationRevision: core.snapshot.ConfigurationRevision, ExpectedEffectiveRevision: core.snapshot.Effective.Revision,
		Observation: core.observation,
	}, nil
}

func (core *workflowCoreFake) VerifyEffective(context.Context, coreinboundcontrol.VerifyEffectiveRequestV1) (coreinboundcontrol.EffectiveVerificationV1, error) {
	core.record("core:verify")
	core.verifyCalls++
	if core.failVerify {
		return coreinboundcontrol.EffectiveVerificationV1{}, errors.New("verify")
	}
	core.checkpoint.State = coreinboundcontrol.CheckpointStateEffectiveVerified
	core.checkpoint.ProofDigest = strings.Repeat("d", 64)
	return coreinboundcontrol.EffectiveVerificationV1{
		CheckpointID: core.checkpoint.CheckpointID, ConfigurationRevision: core.snapshot.ConfigurationRevision,
		EffectiveRevision: core.snapshot.Effective.Revision, Verified: true, ProofDigest: core.checkpoint.ProofDigest, Observation: core.observation,
	}, nil
}

func (core *workflowCoreFake) RestoreCheckpoint(context.Context, coreinboundcontrol.RestoreCheckpointRequestV1) (coreinboundcontrol.RestoreCheckpointResultV1, error) {
	core.record("core:restore")
	core.restoreCalls++
	if core.failRestore {
		return coreinboundcontrol.RestoreCheckpointResultV1{}, errors.New("restore")
	}
	core.snapshot.ConfigurationRevision = core.checkpoint.BeforeConfigurationRevision
	core.snapshot.Effective.Revision = core.beforeEffective
	if core.badRestoreVerify {
		core.snapshot.Effective.Revision = strings.Repeat("0", 64)
	}
	core.checkpoint.State = coreinboundcontrol.CheckpointStateRestoredVerified
	core.checkpoint.CurrentConfigurationRevision = core.snapshot.ConfigurationRevision
	core.checkpoint.CurrentEffectiveRevision = core.snapshot.Effective.Revision
	core.checkpoint.ProofDigest = strings.Repeat("e", 64)
	return coreinboundcontrol.RestoreCheckpointResultV1{
		CheckpointID: core.checkpoint.CheckpointID, RestoredConfigurationRevision: core.snapshot.ConfigurationRevision,
		RestoredEffectiveRevision: core.snapshot.Effective.Revision, ProofDigest: core.checkpoint.ProofDigest,
	}, nil
}

func (core *workflowCoreFake) ReleaseCheckpoint(context.Context, coreinboundcontrol.ReleaseCheckpointRequestV1) (coreinboundcontrol.CheckpointReleaseV1, error) {
	core.record("core:release")
	core.releaseCalls++
	core.checkpoint.State = coreinboundcontrol.CheckpointStateReleased
	return coreinboundcontrol.CheckpointReleaseV1{CheckpointID: core.checkpoint.CheckpointID, ReleasedAt: time.Unix(1_000, 0).UTC()}, nil
}

func (core *workflowCoreFake) record(value string) {
	if core.events != nil {
		*core.events = append(*core.events, value)
	}
}

type workflowProviderFake struct {
	now          time.Time
	target       neutralfallback.FallbackTargetV2
	reservation  neutralfallback.ProviderTargetReservationV1
	events       *[]string
	revision     int
	failActivate bool
	failFence    bool
	failRelease  bool
}

func (provider *workflowProviderFake) ProviderID() string { return "fixture-provider" }
func (provider *workflowProviderFake) InventoryV2(context.Context, neutralfallback.InventoryV2Request) (neutralfallback.InventoryV2Result, *neutralfallback.ProviderContractError) {
	provider.record("provider:inventory")
	return neutralfallback.InventoryV2Result{Targets: []neutralfallback.FallbackTargetV2{provider.target}}, nil
}
func (provider *workflowProviderFake) ResolveV2(_ context.Context, reference neutralfallback.FallbackTargetReferenceV2) (neutralfallback.ResolveV2Result, *neutralfallback.ProviderContractError) {
	provider.record("provider:resolve")
	current, _ := neutralfallback.ReferenceV2FromTarget(provider.target)
	if current != reference {
		return neutralfallback.ResolveV2Result{}, providerTestError(neutralfallback.ProviderErrorStale)
	}
	return neutralfallback.ResolveV2Result{Target: provider.target}, nil
}
func (provider *workflowProviderFake) Reserve(_ context.Context, request neutralfallback.ReserveRequestV1) (neutralfallback.ReservationResultV1, *neutralfallback.ProviderContractError) {
	provider.record("provider:reserve")
	provider.revision++
	provider.reservation = neutralfallback.ProviderTargetReservationV1{
		Schema: neutralfallback.ProviderTargetReservationSchemaV1, ReservationID: "reservation-one", ReservationRevision: provider.nextRevision(),
		HolderID: request.HolderID, Purpose: request.Purpose, ExactTargetReference: request.ExactTargetReference,
		State: neutralfallback.ReservationReserved, IssuedAt: provider.now.Unix(), RenewedAt: provider.now.Unix(),
		FreshnessExpiresAt: provider.now.Add(time.Duration(request.FreshnessDurationSecs) * time.Second).Unix(),
	}
	provider.setUsed(provider.target.Capacity.ReservationSlotsUsed + 1)
	return neutralfallback.ReservationResultV1{Reservation: provider.reservation}, nil
}
func (provider *workflowProviderFake) FenceForMutation(_ context.Context, request neutralfallback.ReservationMutationRequestV1) (neutralfallback.ReservationResultV1, *neutralfallback.ProviderContractError) {
	provider.record("provider:fence")
	if provider.failFence {
		return neutralfallback.ReservationResultV1{}, providerTestError(neutralfallback.ProviderErrorInternal)
	}
	if request.ExpectedRevision != provider.reservation.ReservationRevision || provider.reservation.State != neutralfallback.ReservationReserved {
		return neutralfallback.ReservationResultV1{}, providerTestError(neutralfallback.ProviderErrorStale)
	}
	provider.reservation.State = neutralfallback.ReservationMutationPending
	provider.reservation.ReservationRevision = provider.nextRevision()
	return neutralfallback.ReservationResultV1{Reservation: provider.reservation}, nil
}
func (provider *workflowProviderFake) Activate(_ context.Context, request neutralfallback.ReservationMutationRequestV1) (neutralfallback.ReservationResultV1, *neutralfallback.ProviderContractError) {
	provider.record("provider:activate")
	if provider.failActivate {
		return neutralfallback.ReservationResultV1{}, providerTestError(neutralfallback.ProviderErrorInternal)
	}
	if request.ExpectedRevision != provider.reservation.ReservationRevision || provider.reservation.State != neutralfallback.ReservationMutationPending {
		return neutralfallback.ReservationResultV1{}, providerTestError(neutralfallback.ProviderErrorStale)
	}
	provider.reservation.State = neutralfallback.ReservationActive
	provider.reservation.ReservationRevision = provider.nextRevision()
	provider.reservation.RenewedAt = provider.now.Add(time.Second).Unix()
	provider.reservation.FreshnessExpiresAt = provider.reservation.RenewedAt + int64(request.FreshnessDurationSecs)
	return neutralfallback.ReservationResultV1{Reservation: provider.reservation}, nil
}
func (provider *workflowProviderFake) Renew(context.Context, neutralfallback.ReservationMutationRequestV1) (neutralfallback.ReservationResultV1, *neutralfallback.ProviderContractError) {
	return neutralfallback.ReservationResultV1{}, providerTestError(neutralfallback.ProviderErrorInvalid)
}
func (provider *workflowProviderFake) Release(_ context.Context, request neutralfallback.ReleaseReservationRequestV1) (neutralfallback.ReservationResultV1, *neutralfallback.ProviderContractError) {
	provider.record("provider:release")
	if provider.failRelease {
		return neutralfallback.ReservationResultV1{}, providerTestError(neutralfallback.ProviderErrorInternal)
	}
	if request.ExpectedRevision != provider.reservation.ReservationRevision || provider.reservation.State == neutralfallback.ReservationReleased {
		return neutralfallback.ReservationResultV1{}, providerTestError(neutralfallback.ProviderErrorStale)
	}
	provider.reservation.State = neutralfallback.ReservationReleased
	provider.reservation.ReservationRevision = provider.nextRevision()
	provider.reservation.ReleasedAt = max(provider.now.Add(2*time.Second).Unix(), provider.reservation.RenewedAt)
	provider.setUsed(provider.target.Capacity.ReservationSlotsUsed - 1)
	return neutralfallback.ReservationResultV1{Reservation: provider.reservation}, nil
}
func (provider *workflowProviderFake) GetReservation(context.Context, neutralfallback.GetReservationRequestV1) (neutralfallback.ReservationResultV1, *neutralfallback.ProviderContractError) {
	provider.record("provider:get")
	if provider.reservation.ReservationID == "" {
		return neutralfallback.ReservationResultV1{}, providerTestError(neutralfallback.ProviderErrorNotFound)
	}
	return neutralfallback.ReservationResultV1{Reservation: provider.reservation}, nil
}
func (provider *workflowProviderFake) ListReservations(context.Context, neutralfallback.ListReservationsQueryV1) (neutralfallback.ListReservationsResultV1, *neutralfallback.ProviderContractError) {
	if provider.reservation.ReservationID == "" {
		return neutralfallback.ListReservationsResultV1{}, nil
	}
	return neutralfallback.ListReservationsResultV1{Reservations: []neutralfallback.ProviderTargetReservationV1{provider.reservation}}, nil
}
func (provider *workflowProviderFake) nextRevision() string {
	provider.revision++
	return fmt.Sprintf("reservation-revision-%d", provider.revision)
}
func (provider *workflowProviderFake) setUsed(used uint32) {
	provider.target.Capacity.ReservationSlotsUsed = used
	provider.target.Capacity.Revision = ""
	provider.target.CanonicalTargetRevision = ""
	provider.target, _ = neutralfallback.FinalizeFallbackTargetV2(provider.target)
}
func (provider *workflowProviderFake) record(value string) {
	if provider.events != nil {
		*provider.events = append(*provider.events, value)
	}
}
func providerTestError(class neutralfallback.ProviderErrorClass) *neutralfallback.ProviderContractError {
	return &neutralfallback.ProviderContractError{Class: class, ReasonCode: "injected_failure"}
}

type journalRecorder struct {
	*repository.Repository
	events     *[]string
	failStages map[repository.NativeJournalStage]int
}

func (journal journalRecorder) CreateNativeFallbackOperation(ctx context.Context, model repository.NativeFallbackOperationModel) (repository.NativeFallbackOperationModel, error) {
	*journal.events = append(*journal.events, "journal:create")
	return journal.Repository.CreateNativeFallbackOperation(ctx, model)
}
func (journal journalRecorder) AdvanceNativeFallbackOperation(ctx context.Context, update repository.NativeFallbackJournalUpdate) (repository.NativeFallbackOperationModel, error) {
	*journal.events = append(*journal.events, "journal:"+string(update.Stage))
	if journal.failStages[update.Stage] > 0 {
		journal.failStages[update.Stage]--
		return repository.NativeFallbackOperationModel{}, errors.New("injected journal failure")
	}
	return journal.Repository.AdvanceNativeFallbackOperation(ctx, update)
}

type artifactWriterFake struct{ events *[]string }

func (writer artifactWriterFake) WriteRevision(_ context.Context, operationID, revision string, _ map[string][]byte) (repository.ArtifactModel, error) {
	*writer.events = append(*writer.events, "artifact:write")
	return repository.ArtifactModel{OperationID: operationID, Revision: revision, ManifestSHA256: strings.Repeat("a", 64)}, nil
}

type markerFake struct {
	events     *[]string
	failMark   bool
	failVerify bool
}

func (marker markerFake) MarkMutation(string, string) error {
	*marker.events = append(*marker.events, "artifact:marker")
	if marker.failMark {
		return errors.New("marker")
	}
	return nil
}

func (marker markerFake) VerifyRevision(revision, expectedManifestSHA string) (protectionartifacts.Manifest, error) {
	*marker.events = append(*marker.events, "artifact:verify")
	if marker.failVerify || expectedManifestSHA == "" {
		return protectionartifacts.Manifest{}, errors.New("artifact integrity")
	}
	return protectionartifacts.Manifest{
		Version: protectionartifacts.ManifestVersion, OperationID: strings.TrimPrefix(revision, "native-"), Revision: revision,
	}, nil
}

type workflowTestFixture struct {
	now      time.Time
	db       *gorm.DB
	repo     *repository.Repository
	journal  journalRecorder
	manager  *protectionoperations.Manager
	core     *workflowCoreFake
	provider *workflowProviderFake
	registry *neutralfallback.Registry
	planner  Planner
	workflow *Workflow
	plan     domain.NativeFallbackPlanV1
	request  PlanRequestV1
	events   *[]string
}

func newWorkflowFixture(t *testing.T) *workflowTestFixture {
	t.Helper()
	now := time.Unix(10_000, 0).UTC()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "-")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	events := []string{}
	identity := exactRuntimeIdentity()
	snapshot := vlessSnapshot(identity)
	reader := &fakeCoreReader{identity: identity, snapshot: snapshot, previewAt: now.Add(2 * time.Minute)}
	core := &workflowCoreFake{
		fakeCoreReader: reader, beforeEffective: snapshot.Effective.Revision, events: &events,
		observation: coreinboundcontrol.RuntimeInboundObservationV1{RuntimeAvailable: true, Tag: snapshot.Tag, Type: snapshot.Type, OptionsDigest: strings.Repeat("6", 64), ManagerGeneration: 7, MatchingInboundCount: 1},
	}
	target := targetFixture(t, now, neutralfallback.TransportSecurityTLS, []neutralfallback.ApplicationProtocol{neutralfallback.ApplicationProtocolHTTP11, neutralfallback.ApplicationProtocolHTTP2}, []string{"decoy.example"})
	reference, _ := neutralfallback.ReferenceV2FromTarget(target)
	request := requestFor(snapshot, reference)
	targetReader := &fakeTargetReader{target: target}
	management := &fakeManagementReader{result: ManagementIsolationResultV1{State: "ISOLATED", Revision: strings.Repeat("f", 64), ExpiresAt: now.Add(2 * time.Minute)}}
	planner := Planner{Core: core, Targets: targetReader, Management: management, Now: func() time.Time { return now }}
	plan, err := planner.Plan(context.Background(), request)
	if err != nil || !plan.Eligible {
		t.Fatalf("plan: %#v %v", plan, err)
	}
	provider := &workflowProviderFake{now: now, target: target, events: &events}
	registry := neutralfallback.NewRegistry()
	unregister, err := registry.RegisterV2(provider)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unregister)
	manager := protectionoperations.NewManager(repo, protectionoperations.Options{Now: func() time.Time { return now }, Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil }})
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	journal := journalRecorder{Repository: repo, events: &events, failStages: make(map[repository.NativeJournalStage]int)}
	workflow := &Workflow{
		Operations: manager, Journal: journal, Planner: planner, Core: core, Providers: registry,
		Artifacts: artifactWriterFake{events: &events}, Marker: markerFake{events: &events}, Now: func() time.Time { return now },
	}
	return &workflowTestFixture{now: now, db: db, repo: repo, journal: journal, manager: manager, core: core, provider: provider, registry: registry, planner: planner, workflow: workflow, plan: plan, request: request, events: &events}
}

func (fixture *workflowTestFixture) prepare(t *testing.T) WorkflowResultV1 {
	t.Helper()
	result, err := fixture.workflow.Prepare(context.Background(), PrepareWorkflowRequestV1{
		Actor: "admin", IdempotencyKey: "prepare-" + strings.ReplaceAll(t.Name(), "/", "-"),
		Confirmation: "PREPARE NATIVE FALLBACK " + fixture.plan.PlanDigest, Plan: fixture.plan, PlanRequest: fixture.request,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !targetStillMatchesPlan(fixture.provider.target, fixture.plan, fixture.now, true) {
		t.Fatalf("reserved target no longer matches plan: target=%#v plan=%#v", fixture.provider.target, fixture.plan.Target)
	}
	return result
}

func TestPrepareIdempotencyReplayReturnsSameOperationWithoutSecondPreparation(t *testing.T) {
	fixture := newWorkflowFixture(t)
	request := PrepareWorkflowRequestV1{
		Actor: "admin", IdempotencyKey: "prepare-idempotency-replay",
		Confirmation: "PREPARE NATIVE FALLBACK " + fixture.plan.PlanDigest,
		Plan:         fixture.plan, PlanRequest: fixture.request,
	}
	first, err := fixture.workflow.Prepare(context.Background(), request)
	if err != nil {
		t.Fatalf("first Prepare: %v", err)
	}
	replayed, err := fixture.workflow.Prepare(context.Background(), request)
	if err != nil {
		t.Fatalf("replayed Prepare: %v", err)
	}
	if replayed.Operation.OperationID != first.Operation.OperationID {
		t.Fatalf("replay operation=%q, want %q", replayed.Operation.OperationID, first.Operation.OperationID)
	}
	for _, event := range []string{"journal:create", "provider:reserve", "core:prepare", "artifact:write", "journal:prepared"} {
		if countEvent(*fixture.events, event) != 1 {
			t.Fatalf("event %q repeated during replay: %v", event, *fixture.events)
		}
	}
}

func TestJournaledWorkflowPrepareApplyHealthOrderingAndNoPartialApplied(t *testing.T) {
	fixture := newWorkflowFixture(t)
	prepared := fixture.prepare(t)
	if prepared.State.ActualState != domain.NativeActualPrepared || fixture.provider.reservation.State != neutralfallback.ReservationReserved {
		t.Fatalf("prepared state=%s reservation=%s", prepared.State.ActualState, fixture.provider.reservation.State)
	}
	assertEventOrder(t, *fixture.events, "journal:create", "provider:reserve", "journal:reservation", "core:prepare", "artifact:write", "journal:prepared")
	applyRequest := ApplyWorkflowRequestV1{
		Actor: "admin", IdempotencyKey: "apply-replay-key", OperationID: prepared.Operation.OperationID, OperationRevision: prepared.Operation.Revision,
		PlanDigest: fixture.plan.PlanDigest, ProviderReservationRevision: prepared.Operation.ProviderReservationRevision,
		ExpectedState: domain.NativeActualPrepared, Confirmed: true,
	}
	result, err := fixture.workflow.Apply(context.Background(), applyRequest)
	if err != nil {
		t.Fatalf("Apply: %v\nevents=%v", err, *fixture.events)
	}
	if result.State.ActualState != domain.NativeActualApplied || fixture.provider.reservation.State != neutralfallback.ReservationActive {
		t.Fatalf("applied state=%s reservation=%s", result.State.ActualState, fixture.provider.reservation.State)
	}
	replayed, err := fixture.workflow.Apply(context.Background(), applyRequest)
	if err != nil || replayed.Operation.OperationID != result.Operation.OperationID || fixture.core.applyCalls != 1 {
		t.Fatalf("apply replay operation=%s err=%v applyCalls=%d", replayed.Operation.OperationID, err, fixture.core.applyCalls)
	}
	conflict := applyRequest
	conflict.ProviderReservationRevision += "-different"
	if _, err := fixture.workflow.Apply(context.Background(), conflict); err == nil || !strings.Contains(err.Error(), "idempotency_conflict") {
		t.Fatalf("conflicting apply replay err=%v", err)
	}
	assertEventOrder(t, *fixture.events, "journal:applying", "artifact:marker", "provider:fence", "journal:fenced", "core:apply", "journal:health", "core:verify", "provider:activate", "journal:applied")
}

func TestManualRollbackReclaimsAppliedFenceAndDelegatesOnce(t *testing.T) {
	fixture := newWorkflowFixture(t)
	prepared := fixture.prepare(t)
	applied, err := fixture.workflow.Apply(context.Background(), ApplyWorkflowRequestV1{
		Actor: "admin", OperationID: prepared.Operation.OperationID, OperationRevision: prepared.Operation.Revision,
		PlanDigest: fixture.plan.PlanDigest, ProviderReservationRevision: prepared.Operation.ProviderReservationRevision,
		ExpectedState: domain.NativeActualPrepared, Confirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rollbackRequest := RollbackWorkflowRequestV1{
		Actor: "admin", IdempotencyKey: "rollback-replay-key", OperationID: applied.Operation.OperationID, OperationRevision: applied.Operation.Revision,
		PlanDigest: applied.Operation.PlanDigest, ProviderReservationRevision: applied.Operation.ProviderReservationRevision, Confirmed: true,
	}
	result, err := fixture.workflow.Rollback(context.Background(), rollbackRequest)
	if err != nil || result.State.ActualState != domain.NativeActualRolledBack || fixture.core.restoreCalls != 1 ||
		fixture.provider.reservation.State != neutralfallback.ReservationReleased {
		t.Fatalf("state=%s err=%v restores=%d reservation=%s", result.State.ActualState, err, fixture.core.restoreCalls, fixture.provider.reservation.State)
	}
	lock, err := fixture.repo.OperationByID(context.Background(), applied.Operation.OperationID)
	if err != nil || lock.State != protectionoperations.StateRolledBack {
		t.Fatalf("lock=%#v err=%v", lock, err)
	}
	replayed, err := fixture.workflow.Rollback(context.Background(), rollbackRequest)
	if err != nil || replayed.Operation.OperationID != result.Operation.OperationID || fixture.core.restoreCalls != 1 || countEvent(*fixture.events, "provider:release") != 1 {
		t.Fatalf("rollback replay operation=%s err=%v restores=%d releases=%d", replayed.Operation.OperationID, err, fixture.core.restoreCalls, countEvent(*fixture.events, "provider:release"))
	}
	conflict := rollbackRequest
	conflict.ProviderReservationRevision += "-different"
	if _, err := fixture.workflow.Rollback(context.Background(), conflict); err == nil || !strings.Contains(err.Error(), "idempotency_conflict") {
		t.Fatalf("conflicting rollback replay err=%v", err)
	}
}

func TestPrepareRejectsExpiredPlanAndCheckpointFailureLeavesNoOrphanAuthority(t *testing.T) {
	t.Run("expired_plan", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		fixture.workflow.Now = func() time.Time { return fixture.plan.ExpiresAt.Add(time.Second) }
		_, err := fixture.workflow.Prepare(context.Background(), PrepareWorkflowRequestV1{
			Actor: "admin", IdempotencyKey: "expired-plan", Confirmation: "PREPARE NATIVE FALLBACK " + fixture.plan.PlanDigest,
			Plan: fixture.plan, PlanRequest: fixture.request,
		})
		if err == nil || countEvent(*fixture.events, "provider:reserve") != 0 || fixture.core.applyCalls != 0 {
			t.Fatalf("err=%v events=%v applyCalls=%d", err, *fixture.events, fixture.core.applyCalls)
		}
	})
	t.Run("checkpoint_failure", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		fixture.core.failPrepare = true
		result, err := fixture.workflow.Prepare(context.Background(), PrepareWorkflowRequestV1{
			Actor: "admin", IdempotencyKey: "checkpoint-failure", Confirmation: "PREPARE NATIVE FALLBACK " + fixture.plan.PlanDigest,
			Plan: fixture.plan, PlanRequest: fixture.request,
		})
		if err == nil || result.State.ActualState != domain.NativeActualNotApplied || fixture.provider.reservation.State != neutralfallback.ReservationReleased || fixture.core.applyCalls != 0 {
			t.Fatalf("state=%s err=%v reservation=%s events=%v", result.State.ActualState, err, fixture.provider.reservation.State, *fixture.events)
		}
		assertEventOrder(t, *fixture.events, "journal:create", "provider:reserve", "journal:reservation", "core:prepare", "provider:release", "journal:cancelled")
	})
	t.Run("artifact_integrity", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		fixture.workflow.Marker = markerFake{events: fixture.events, failVerify: true}
		result, err := fixture.workflow.Prepare(context.Background(), PrepareWorkflowRequestV1{
			Actor: "admin", IdempotencyKey: "artifact-integrity", Confirmation: "PREPARE NATIVE FALLBACK " + fixture.plan.PlanDigest,
			Plan: fixture.plan, PlanRequest: fixture.request,
		})
		if err == nil || result.State.ActualState != domain.NativeActualNotApplied || fixture.provider.reservation.State != neutralfallback.ReservationReleased || fixture.core.releaseCalls != 1 {
			t.Fatalf("state=%s err=%v reservation=%s coreReleases=%d events=%v", result.State.ActualState, err, fixture.provider.reservation.State, fixture.core.releaseCalls, *fixture.events)
		}
	})
}

func TestNativeJournalRejectsIllegalAndStaleTransitionsAtomically(t *testing.T) {
	fixture := newWorkflowFixture(t)
	prepared := fixture.prepare(t)
	for name, update := range map[string]repository.NativeFallbackJournalUpdate{
		"stale revision": {
			OperationID: prepared.Operation.OperationID, ExpectedRevision: prepared.Operation.Revision - 1,
			Stage: repository.NativeJournalApplying, Now: fixture.now,
		},
		"skip health to applied": {
			OperationID: prepared.Operation.OperationID, ExpectedRevision: prepared.Operation.Revision,
			Stage: repository.NativeJournalApplied, Now: fixture.now,
		},
		"rollback before mutation": {
			OperationID: prepared.Operation.OperationID, ExpectedRevision: prepared.Operation.Revision,
			Stage: repository.NativeJournalRollingBack, Now: fixture.now,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fixture.journal.AdvanceNativeFallbackOperation(context.Background(), update); err == nil {
				t.Fatal("illegal journal transition was accepted")
			}
			operation, err := fixture.repo.NativeFallbackOperation(context.Background(), prepared.Operation.OperationID)
			state, stateErr := fixture.repo.NativeFallbackState(context.Background(), prepared.Operation.ResourceID)
			if err != nil || stateErr != nil || operation.Revision != prepared.Operation.Revision || operation.WorkflowState != repository.NativeWorkflowPrepared || state.ActualState != domain.NativeActualPrepared {
				t.Fatalf("operation=%#v state=%#v err=%v stateErr=%v", operation, state, err, stateErr)
			}
		})
	}
}

func TestFailureAfterMarkerRollsBackExactlyOnceAndReleasesAfterDetachment(t *testing.T) {
	fixture := newWorkflowFixture(t)
	prepared := fixture.prepare(t)
	fixture.provider.failActivate = true
	result, err := fixture.workflow.Apply(context.Background(), ApplyWorkflowRequestV1{
		Actor: "admin", OperationID: prepared.Operation.OperationID, OperationRevision: prepared.Operation.Revision,
		PlanDigest: fixture.plan.PlanDigest, ProviderReservationRevision: prepared.Operation.ProviderReservationRevision,
		ExpectedState: domain.NativeActualPrepared, Confirmed: true,
	})
	if err == nil || result.State.ActualState != domain.NativeActualRolledBack || fixture.core.restoreCalls != 1 || fixture.provider.reservation.State != neutralfallback.ReservationReleased {
		t.Fatalf("result=%s err=%v restores=%d reservation=%s events=%v", result.State.ActualState, err, fixture.core.restoreCalls, fixture.provider.reservation.State, *fixture.events)
	}
	assertEventOrder(t, *fixture.events, "core:restore", "provider:release", "core:release", "journal:rolled_back")
}

func TestMarkerFailureNeverCallsCoreApplyAndStillCleansReservedAuthority(t *testing.T) {
	fixture := newWorkflowFixture(t)
	prepared := fixture.prepare(t)
	fixture.workflow.Marker = markerFake{events: fixture.events, failMark: true}
	result, err := fixture.workflow.Apply(context.Background(), ApplyWorkflowRequestV1{
		Actor: "admin", OperationID: prepared.Operation.OperationID, OperationRevision: prepared.Operation.Revision,
		PlanDigest: fixture.plan.PlanDigest, ProviderReservationRevision: prepared.Operation.ProviderReservationRevision,
		ExpectedState: domain.NativeActualPrepared, Confirmed: true,
	})
	if err == nil || fixture.core.applyCalls != 0 || result.State.ActualState != domain.NativeActualRolledBack || fixture.provider.reservation.State != neutralfallback.ReservationReleased {
		t.Fatalf("state=%s err=%v applyCalls=%d reservation=%s", result.State.ActualState, err, fixture.core.applyCalls, fixture.provider.reservation.State)
	}
}

func TestPostMarkerFailureMatrixNeverReportsPartialSuccess(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		configure func(*workflowTestFixture)
	}{
		{name: "provider_fence", configure: func(f *workflowTestFixture) { f.provider.failFence = true }},
		{name: "core_apply", configure: func(f *workflowTestFixture) { f.core.failApply = true }},
		{name: "core_effective", configure: func(f *workflowTestFixture) { f.core.failVerify = true }},
		{name: "target_health", configure: func(f *workflowTestFixture) {
			f.core.afterApply = func() {
				f.provider.target.Health.Readiness = neutralfallback.ReadinessNotReady
				f.provider.target.Health.ReasonCodes = []string{"injected_unavailable"}
				f.provider.target.Health.Revision = ""
				f.provider.target.CanonicalTargetRevision = ""
				f.provider.target, _ = neutralfallback.FinalizeFallbackTargetV2(f.provider.target)
			}
		}},
		{name: "health_state_persist", configure: func(f *workflowTestFixture) { f.journal.failStages[repository.NativeJournalHealth] = 1 }},
		{name: "applied_state_persist", configure: func(f *workflowTestFixture) { f.journal.failStages[repository.NativeJournalApplied] = 1 }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newWorkflowFixture(t)
			prepared := fixture.prepare(t)
			testCase.configure(fixture)
			result, err := fixture.workflow.Apply(context.Background(), ApplyWorkflowRequestV1{
				Actor: "admin", OperationID: prepared.Operation.OperationID, OperationRevision: prepared.Operation.Revision,
				PlanDigest: fixture.plan.PlanDigest, ProviderReservationRevision: prepared.Operation.ProviderReservationRevision,
				ExpectedState: domain.NativeActualPrepared, Confirmed: true,
			})
			if err == nil || result.State.ActualState == domain.NativeActualApplied || fixture.provider.reservation.State != neutralfallback.ReservationReleased {
				t.Fatalf("state=%s err=%v reservation=%s events=%v", result.State.ActualState, err, fixture.provider.reservation.State, *fixture.events)
			}
		})
	}
}

func TestRollbackFailureMatrixRetainsAuthorityCheckpointAndSafeBundle(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		configure func(*workflowTestFixture)
	}{
		{name: "restore", configure: func(f *workflowTestFixture) { f.core.failRestore = true }},
		{name: "previous_effective", configure: func(f *workflowTestFixture) { f.core.badRestoreVerify = true }},
		{name: "provider_release", configure: func(f *workflowTestFixture) { f.provider.failRelease = true }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newWorkflowFixture(t)
			prepared := fixture.prepare(t)
			fixture.provider.failActivate = true
			testCase.configure(fixture)
			result, err := fixture.workflow.Apply(context.Background(), ApplyWorkflowRequestV1{
				Actor: "admin", OperationID: prepared.Operation.OperationID, OperationRevision: prepared.Operation.Revision,
				PlanDigest: fixture.plan.PlanDigest, ProviderReservationRevision: prepared.Operation.ProviderReservationRevision,
				ExpectedState: domain.NativeActualPrepared, Confirmed: true,
			})
			if err == nil || result.State.ActualState != domain.NativeActualRollbackFailed || fixture.provider.reservation.State == neutralfallback.ReservationReleased ||
				fixture.core.checkpoint.State == coreinboundcontrol.CheckpointStateReleased || fixture.core.restoreCalls != 1 {
				t.Fatalf("state=%s err=%v reservation=%s checkpoint=%s restores=%d", result.State.ActualState, err, fixture.provider.reservation.State, fixture.core.checkpoint.State, fixture.core.restoreCalls)
			}
			serialized := strings.ToLower(string(result.Operation.RecoveryBundleJSON))
			if len(serialized) == 0 || len(serialized) > 16<<10 {
				t.Fatalf("unsafe bundle size=%d", len(serialized))
			}
			for _, forbidden := range []string{"password", "private_key", "certificate", "raw_config", "filesystem", "dsn", "environment", "token", "cookie", "http://", "https://"} {
				if strings.Contains(serialized, forbidden) {
					t.Fatalf("recovery bundle contains %q: %s", forbidden, serialized)
				}
			}
		})
	}
}

func TestCancellationAfterPossibleCommitIsAmbiguousAndRetainsAuthority(t *testing.T) {
	fixture := newWorkflowFixture(t)
	prepared := fixture.prepare(t)
	ctx, cancel := context.WithCancel(context.Background())
	fixture.core.cancelAfterCommit = cancel
	result, err := fixture.workflow.Apply(ctx, ApplyWorkflowRequestV1{
		Actor: "admin", OperationID: prepared.Operation.OperationID, OperationRevision: prepared.Operation.Revision,
		PlanDigest: fixture.plan.PlanDigest, ProviderReservationRevision: prepared.Operation.ProviderReservationRevision,
		ExpectedState: domain.NativeActualPrepared, Confirmed: true,
	})
	if err == nil || result.State.ActualState != domain.NativeActualReconcileRequired || fixture.provider.reservation.State != neutralfallback.ReservationMutationPending ||
		fixture.core.restoreCalls != 0 || fixture.core.releaseCalls != 0 || fixture.provider.reservation.State == neutralfallback.ReservationReleased {
		t.Fatalf("state=%s err=%v reservation=%s restores=%d releases=%d", result.State.ActualState, err, fixture.provider.reservation.State, fixture.core.restoreCalls, fixture.core.releaseCalls)
	}
}

func TestRestartClassificationResumesHealthWithoutDuplicateApplyOrActivation(t *testing.T) {
	fixture := newWorkflowFixture(t)
	prepared := fixture.prepare(t)
	operation, err := fixture.journal.AdvanceNativeFallbackOperation(context.Background(), repository.NativeFallbackJournalUpdate{
		OperationID: prepared.Operation.OperationID, ExpectedRevision: prepared.Operation.Revision, Stage: repository.NativeJournalApplying, Now: fixture.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	fenced, err := fenceProvider(context.Background(), fixture.provider, neutralfallback.ReservationMutationRequestV1{
		RequestID: operation.OperationID + "-fence", ReservationID: fixture.provider.reservation.ReservationID, ExpectedRevision: fixture.provider.reservation.ReservationRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err = fixture.journal.AdvanceNativeFallbackOperation(context.Background(), repository.NativeFallbackJournalUpdate{
		OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalFenced, Reservation: &fenced, Now: fixture.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.core.ApplyFallbackPatch(context.Background(), coreinboundcontrol.ApplyFallbackPatchRequestV1{CheckpointID: operation.CoreCheckpointID, ExpectedBeforeRevision: operation.BeforeConfigurationRevision, ApprovedEndpoint: approvedEndpoint(fixture.provider.target)}); err != nil {
		t.Fatal(err)
	}
	lock, _ := fixture.repo.OperationByID(context.Background(), operation.OperationID)
	decision, err := fixture.workflow.Reconcile(context.Background(), lock)
	if err != nil || decision.State != protectionoperations.StateApplied || fixture.core.applyCalls != 1 || fixture.provider.reservation.State != neutralfallback.ReservationActive {
		t.Fatalf("decision=%#v err=%v applyCalls=%d reservation=%s", decision, err, fixture.core.applyCalls, fixture.provider.reservation.State)
	}
	activationCount := countEvent(*fixture.events, "provider:activate")
	operation, _ = fixture.repo.NativeFallbackOperation(context.Background(), operation.OperationID)
	decision, err = fixture.workflow.Reconcile(context.Background(), lock)
	if err != nil || decision.State != protectionoperations.StateApplied || fixture.core.applyCalls != 1 || countEvent(*fixture.events, "provider:activate") != activationCount {
		t.Fatalf("repeat decision=%#v err=%v events=%v", decision, err, *fixture.events)
	}
}

func TestHistoricalAppliedRequiresFreshVerificationAndRetainsAuthorityOnFailure(t *testing.T) {
	t.Run("restored_nonterminal_remains_untrusted_without_mutation", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		prepared := fixture.prepare(t)
		if err := repository.ReconcileRestoredNativeFallbackRecords(context.Background(), fixture.db, fixture.now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		operation, err := fixture.repo.NativeFallbackOperation(context.Background(), prepared.Operation.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		lock, err := fixture.repo.OperationByID(context.Background(), operation.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		decision, err := fixture.workflow.Reconcile(context.Background(), lock)
		if err != nil || decision.State != protectionoperations.StateReconcileRequired || fixture.provider.reservation.State != neutralfallback.ReservationReserved ||
			fixture.core.applyCalls != 0 || fixture.core.restoreCalls != 0 || fixture.core.releaseCalls != 0 || countEvent(*fixture.events, "provider:release") != 0 {
			t.Fatalf("decision=%#v err=%v reservation=%s apply=%d restore=%d coreRelease=%d events=%v", decision, err, fixture.provider.reservation.State, fixture.core.applyCalls, fixture.core.restoreCalls, fixture.core.releaseCalls, *fixture.events)
		}
	})

	t.Run("restored_applied_is_reverified_without_repeating_mutations", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		prepared := fixture.prepare(t)
		result, err := fixture.workflow.Apply(context.Background(), ApplyWorkflowRequestV1{
			Actor: "admin", OperationID: prepared.Operation.OperationID, OperationRevision: prepared.Operation.Revision,
			PlanDigest: fixture.plan.PlanDigest, ProviderReservationRevision: prepared.Operation.ProviderReservationRevision,
			ExpectedState: domain.NativeActualPrepared, Confirmed: true,
		})
		if err != nil || result.State.ActualState != domain.NativeActualApplied {
			t.Fatalf("apply state=%s err=%v", result.State.ActualState, err)
		}
		verifyCalls := fixture.core.verifyCalls
		activationCalls := countEvent(*fixture.events, "provider:activate")
		if err := repository.ReconcileRestoredNativeFallbackRecords(context.Background(), fixture.db, fixture.now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		operation, err := fixture.repo.NativeFallbackOperation(context.Background(), result.Operation.OperationID)
		if err != nil || operation.WorkflowState != repository.NativeWorkflowReconcileRequired || operation.RecoveryClassification != repository.NativeRecoveryRestoredUntrusted {
			t.Fatalf("restored operation=%#v err=%v", operation, err)
		}
		lock, err := fixture.repo.OperationByID(context.Background(), operation.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		decision, err := fixture.workflow.Reconcile(context.Background(), lock)
		if err != nil || decision.State != protectionoperations.StateApplied || fixture.core.verifyCalls != verifyCalls+1 ||
			fixture.core.applyCalls != 1 || fixture.core.restoreCalls != 0 || fixture.core.releaseCalls != 0 ||
			countEvent(*fixture.events, "provider:activate") != activationCalls || countEvent(*fixture.events, "provider:release") != 0 {
			t.Fatalf("decision=%#v err=%v apply=%d verify=%d restore=%d coreRelease=%d events=%v", decision, err, fixture.core.applyCalls, fixture.core.verifyCalls, fixture.core.restoreCalls, fixture.core.releaseCalls, *fixture.events)
		}
		operation, err = fixture.repo.NativeFallbackOperation(context.Background(), operation.OperationID)
		state, stateErr := fixture.repo.NativeFallbackState(context.Background(), operation.ResourceID)
		if err != nil || stateErr != nil || operation.WorkflowState != repository.NativeWorkflowApplied || operation.RecoveryClassification != "" || state.ActualState != domain.NativeActualApplied {
			t.Fatalf("operation=%#v state=%#v err=%v stateErr=%v", operation, state, err, stateErr)
		}
	})

	t.Run("failed_reverification_keeps_checkpoint_and_provider_authority", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		prepared := fixture.prepare(t)
		result, err := fixture.workflow.Apply(context.Background(), ApplyWorkflowRequestV1{
			Actor: "admin", OperationID: prepared.Operation.OperationID, OperationRevision: prepared.Operation.Revision,
			PlanDigest: fixture.plan.PlanDigest, ProviderReservationRevision: prepared.Operation.ProviderReservationRevision,
			ExpectedState: domain.NativeActualPrepared, Confirmed: true,
		})
		if err != nil || result.State.ActualState != domain.NativeActualApplied {
			t.Fatalf("apply state=%s err=%v", result.State.ActualState, err)
		}
		fixture.core.failVerify = true
		lock, err := fixture.repo.OperationByID(context.Background(), result.Operation.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		decision, err := fixture.workflow.Reconcile(context.Background(), lock)
		operation, operationErr := fixture.repo.NativeFallbackOperation(context.Background(), result.Operation.OperationID)
		if err != nil || operationErr != nil || decision.State != protectionoperations.StateReconcileRequired ||
			operation.WorkflowState != repository.NativeWorkflowReconcileRequired || fixture.provider.reservation.State != neutralfallback.ReservationActive ||
			fixture.core.restoreCalls != 0 || fixture.core.releaseCalls != 0 || countEvent(*fixture.events, "provider:release") != 0 {
			t.Fatalf("decision=%#v err=%v operation=%#v operationErr=%v reservation=%s restore=%d coreRelease=%d events=%v", decision, err, operation, operationErr, fixture.provider.reservation.State, fixture.core.restoreCalls, fixture.core.releaseCalls, *fixture.events)
		}
	})
}

func TestRestartRejectsStaleProviderAuthorityAndTamperedMirrorWithoutMutation(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*workflowTestFixture, WorkflowResultV1)
	}{
		{name: "stale_active_authority", mutate: func(f *workflowTestFixture, _ WorkflowResultV1) {
			f.workflow.Now = func() time.Time { return time.Unix(f.provider.reservation.FreshnessExpiresAt+1, 0).UTC() }
		}},
		{name: "deleted_mirror", mutate: func(f *workflowTestFixture, result WorkflowResultV1) {
			if err := f.db.Where("operation_id = ?", result.Operation.OperationID).Delete(&repository.FallbackTargetLeaseModel{}).Error; err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newWorkflowFixture(t)
			prepared := fixture.prepare(t)
			result, err := fixture.workflow.Apply(context.Background(), ApplyWorkflowRequestV1{
				Actor: "admin", OperationID: prepared.Operation.OperationID, OperationRevision: prepared.Operation.Revision,
				PlanDigest: fixture.plan.PlanDigest, ProviderReservationRevision: prepared.Operation.ProviderReservationRevision,
				ExpectedState: domain.NativeActualPrepared, Confirmed: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			testCase.mutate(fixture, result)
			lock, err := fixture.repo.OperationByID(context.Background(), result.Operation.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			decision, err := fixture.workflow.Reconcile(context.Background(), lock)
			if err != nil || decision.State != protectionoperations.StateReconcileRequired || fixture.provider.reservation.State != neutralfallback.ReservationActive ||
				fixture.core.restoreCalls != 0 || fixture.core.releaseCalls != 0 || countEvent(*fixture.events, "provider:release") != 0 {
				t.Fatalf("decision=%#v err=%v reservation=%s restore=%d coreRelease=%d events=%v", decision, err, fixture.provider.reservation.State, fixture.core.restoreCalls, fixture.core.releaseCalls, *fixture.events)
			}
		})
	}
}

func TestRestartFinishesInterruptedRollbackWithoutDuplicateRestoreOrRelease(t *testing.T) {
	t.Run("final_journal_failed_after_cleanup", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		prepared := fixture.prepare(t)
		fixture.provider.failActivate = true
		fixture.journal.failStages[repository.NativeJournalRolledBack] = 1
		result, err := fixture.workflow.Apply(context.Background(), ApplyWorkflowRequestV1{
			Actor: "admin", OperationID: prepared.Operation.OperationID, OperationRevision: prepared.Operation.Revision,
			PlanDigest: fixture.plan.PlanDigest, ProviderReservationRevision: prepared.Operation.ProviderReservationRevision,
			ExpectedState: domain.NativeActualPrepared, Confirmed: true,
		})
		if err == nil || result.Operation.WorkflowState != repository.NativeWorkflowRollingBack || fixture.provider.reservation.State != neutralfallback.ReservationReleased || fixture.core.checkpoint.State != coreinboundcontrol.CheckpointStateReleased {
			t.Fatalf("operation=%#v err=%v reservation=%s checkpoint=%s", result.Operation, err, fixture.provider.reservation.State, fixture.core.checkpoint.State)
		}
		providerReleases := countEvent(*fixture.events, "provider:release")
		coreReleases, restores := fixture.core.releaseCalls, fixture.core.restoreCalls
		lock, err := fixture.repo.OperationByID(context.Background(), result.Operation.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		decision, err := fixture.workflow.Reconcile(context.Background(), lock)
		if err != nil || decision.State != protectionoperations.StateRolledBack || fixture.core.restoreCalls != restores || fixture.core.releaseCalls != coreReleases ||
			countEvent(*fixture.events, "provider:release") != providerReleases {
			t.Fatalf("decision=%#v err=%v restores=%d coreReleases=%d providerReleases=%d events=%v", decision, err, fixture.core.restoreCalls, fixture.core.releaseCalls, countEvent(*fixture.events, "provider:release"), *fixture.events)
		}
	})

	t.Run("rollback_journal_failure_is_retryable_not_false_terminal", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		prepared := fixture.prepare(t)
		operation, err := fixture.journal.AdvanceNativeFallbackOperation(context.Background(), repository.NativeFallbackJournalUpdate{
			OperationID: prepared.Operation.OperationID, ExpectedRevision: prepared.Operation.Revision, Stage: repository.NativeJournalApplying, Now: fixture.now,
		})
		if err != nil {
			t.Fatal(err)
		}
		fixture.journal.failStages[repository.NativeJournalRollingBack] = 1
		lock, err := fixture.repo.OperationByID(context.Background(), operation.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		if decision, reconcileErr := fixture.workflow.Reconcile(context.Background(), lock); reconcileErr == nil || decision.State != "" {
			t.Fatalf("journal failure was hidden: decision=%#v err=%v", decision, reconcileErr)
		}
		operation, err = fixture.repo.NativeFallbackOperation(context.Background(), operation.OperationID)
		if err != nil || operation.WorkflowState != repository.NativeWorkflowApplying || fixture.provider.reservation.State != neutralfallback.ReservationReserved {
			t.Fatalf("operation=%#v err=%v reservation=%s", operation, err, fixture.provider.reservation.State)
		}
		decision, err := fixture.workflow.Reconcile(context.Background(), lock)
		if err != nil || decision.State != protectionoperations.StateRolledBack || fixture.core.restoreCalls != 0 || fixture.core.releaseCalls != 1 || countEvent(*fixture.events, "provider:release") != 1 {
			t.Fatalf("decision=%#v err=%v restore=%d coreRelease=%d events=%v", decision, err, fixture.core.restoreCalls, fixture.core.releaseCalls, *fixture.events)
		}
	})
}

func TestRestartCancelsCrashBeforeOperationJournalWhenNoProviderAuthorityExists(t *testing.T) {
	fixture := newWorkflowFixture(t)
	acquired, err := fixture.manager.Acquire(context.Background(), protectionoperations.AcquireRequest{
		Kind: protectionoperations.KindNativeFallback, ResourceID: fixture.plan.Resource.ResourceID,
		IdempotencyKey: "crash-before-operation", PlanRevision: fixture.plan.PlanDigest, Actor: "admin",
		InitialState: protectionoperations.StatePrepared,
	})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := fixture.workflow.Reconcile(context.Background(), acquired.Operation)
	if err != nil || decision.State != protectionoperations.StateCancelled || countEvent(*fixture.events, "provider:release") != 0 || fixture.core.applyCalls != 0 {
		t.Fatalf("decision=%#v err=%v events=%v", decision, err, *fixture.events)
	}
}

func TestRestartAdoptsOrphanReservationAndLostCheckpointJournalIdempotently(t *testing.T) {
	for _, testCase := range []struct {
		name             string
		checkpointExists bool
	}{
		{name: "after_reserve", checkpointExists: false},
		{name: "after_checkpoint", checkpointExists: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newWorkflowFixture(t)
			prepared := fixture.prepare(t)
			if !testCase.checkpointExists {
				fixture.core.checkpoint = coreinboundcontrol.CheckpointStatusV1{}
			}
			if err := fixture.db.Model(&repository.NativeFallbackOperationModel{}).Where("operation_id = ?", prepared.Operation.OperationID).Updates(map[string]any{
				"workflow_state": repository.NativeWorkflowPreparing, "provider_reservation_id": "", "provider_reservation_revision": "",
				"core_checkpoint_id": "", "core_checkpoint_digest": "", "checkpoint_release_proof": "",
				"artifact_revision": "", "artifact_manifest_digest": "", "prepared_at": nil,
			}).Error; err != nil {
				t.Fatal(err)
			}
			if err := fixture.db.Where("operation_id = ?", prepared.Operation.OperationID).Delete(&repository.FallbackTargetLeaseModel{}).Error; err != nil {
				t.Fatal(err)
			}
			operation, err := fixture.repo.NativeFallbackOperation(context.Background(), prepared.Operation.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			lock, err := fixture.repo.OperationByID(context.Background(), operation.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			decision, err := fixture.workflow.Reconcile(context.Background(), lock)
			if err != nil || decision.State != protectionoperations.StateCancelled || fixture.provider.reservation.State != neutralfallback.ReservationReleased || fixture.core.applyCalls != 0 {
				t.Fatalf("decision=%#v err=%v reservation=%s events=%v", decision, err, fixture.provider.reservation.State, *fixture.events)
			}
			if testCase.checkpointExists && (countEvent(*fixture.events, "core:find") != 1 || fixture.core.releaseCalls != 1) {
				t.Fatalf("lost checkpoint was not adopted exactly once: events=%v releases=%d", *fixture.events, fixture.core.releaseCalls)
			}
			if !testCase.checkpointExists && fixture.core.releaseCalls != 0 {
				t.Fatalf("missing checkpoint was invented: releases=%d", fixture.core.releaseCalls)
			}
			operation, err = fixture.repo.NativeFallbackOperation(context.Background(), operation.OperationID)
			if err != nil || operation.WorkflowState != repository.NativeWorkflowCancelled {
				t.Fatalf("operation=%#v err=%v", operation, err)
			}
			mirror, err := fixture.repo.ReservationMirror(context.Background(), operation.OperationID)
			if err != nil || mirror.State != string(neutralfallback.ReservationReleased) {
				t.Fatalf("released mirror=%#v err=%v", mirror, err)
			}
		})
	}
}

func TestRestartDriftMissingCheckpointAndProviderAbsenceFailClosed(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*workflowTestFixture)
	}{
		{name: "drift", mutate: func(f *workflowTestFixture) { f.core.snapshot.ConfigurationRevision = strings.Repeat("0", 64) }},
		{name: "checkpoint_missing", mutate: func(f *workflowTestFixture) { f.core.missingCheckpoint = true }},
		{name: "provider_absent", mutate: func(f *workflowTestFixture) {
			f.registry = neutralfallback.NewRegistry()
			f.workflow.Providers = f.registry
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newWorkflowFixture(t)
			prepared := fixture.prepare(t)
			testCase.mutate(fixture)
			lock, _ := fixture.repo.OperationByID(context.Background(), prepared.Operation.OperationID)
			decision, err := fixture.workflow.Reconcile(context.Background(), lock)
			if err != nil || decision.State != protectionoperations.StateReconcileRequired || fixture.provider.reservation.State == neutralfallback.ReservationReleased {
				t.Fatalf("decision=%#v err=%v reservation=%s", decision, err, fixture.provider.reservation.State)
			}
		})
	}
}

func TestRestartClassifiesMarkerAtBeforeAndPreviouslyRestoredAuthority(t *testing.T) {
	t.Run("marker_at_before", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		prepared := fixture.prepare(t)
		operation, err := fixture.journal.AdvanceNativeFallbackOperation(context.Background(), repository.NativeFallbackJournalUpdate{
			OperationID: prepared.Operation.OperationID, ExpectedRevision: prepared.Operation.Revision,
			Stage: repository.NativeJournalApplying, Now: fixture.now,
		})
		if err != nil {
			t.Fatal(err)
		}
		lock, _ := fixture.repo.OperationByID(context.Background(), operation.OperationID)
		decision, err := fixture.workflow.Reconcile(context.Background(), lock)
		if err != nil || decision.State != protectionoperations.StateRolledBack || fixture.core.restoreCalls != 0 ||
			fixture.provider.reservation.State != neutralfallback.ReservationReleased || fixture.core.releaseCalls != 1 {
			t.Fatalf("decision=%#v err=%v restores=%d reservation=%s releases=%d", decision, err, fixture.core.restoreCalls, fixture.provider.reservation.State, fixture.core.releaseCalls)
		}
	})

	t.Run("previous_state_still_guarded", func(t *testing.T) {
		fixture := newWorkflowFixture(t)
		prepared := fixture.prepare(t)
		operation, err := fixture.journal.AdvanceNativeFallbackOperation(context.Background(), repository.NativeFallbackJournalUpdate{
			OperationID: prepared.Operation.OperationID, ExpectedRevision: prepared.Operation.Revision,
			Stage: repository.NativeJournalApplying, Now: fixture.now,
		})
		if err != nil {
			t.Fatal(err)
		}
		fenced, err := fenceProvider(context.Background(), fixture.provider, neutralfallback.ReservationMutationRequestV1{
			RequestID: operation.OperationID + "-fence", ReservationID: fixture.provider.reservation.ReservationID,
			ExpectedRevision: fixture.provider.reservation.ReservationRevision,
		})
		if err != nil {
			t.Fatal(err)
		}
		operation, err = fixture.journal.AdvanceNativeFallbackOperation(context.Background(), repository.NativeFallbackJournalUpdate{
			OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalFenced,
			Reservation: &fenced, Now: fixture.now,
		})
		if err != nil {
			t.Fatal(err)
		}
		mutation, err := fixture.core.ApplyFallbackPatch(context.Background(), coreinboundcontrol.ApplyFallbackPatchRequestV1{
			CheckpointID: operation.CoreCheckpointID, ExpectedBeforeRevision: operation.BeforeConfigurationRevision,
			ApprovedEndpoint: approvedEndpoint(fixture.provider.target),
		})
		if err != nil {
			t.Fatal(err)
		}
		operation, err = fixture.journal.AdvanceNativeFallbackOperation(context.Background(), repository.NativeFallbackJournalUpdate{
			OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalHealth,
			AfterConfigurationRevision: mutation.AfterConfigurationRevision, ExpectedEffectiveRevision: mutation.ExpectedEffectiveRevision,
			ManagerGeneration: mutation.Observation.ManagerGeneration, Now: fixture.now,
		})
		if err != nil {
			t.Fatal(err)
		}
		operation, err = fixture.journal.AdvanceNativeFallbackOperation(context.Background(), repository.NativeFallbackJournalUpdate{
			OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalRollingBack,
			RecoveryClassification: "injected_restart", Now: fixture.now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.core.RestoreCheckpoint(context.Background(), coreinboundcontrol.RestoreCheckpointRequestV1{
			CheckpointID: operation.CoreCheckpointID, ExpectedCurrentRevision: operation.ExpectedAfterRevision,
		}); err != nil {
			t.Fatal(err)
		}
		lock, _ := fixture.repo.OperationByID(context.Background(), operation.OperationID)
		decision, err := fixture.workflow.Reconcile(context.Background(), lock)
		if err != nil || decision.State != protectionoperations.StateRolledBack || fixture.core.restoreCalls != 1 ||
			fixture.provider.reservation.State != neutralfallback.ReservationReleased {
			t.Fatalf("decision=%#v err=%v restores=%d reservation=%s", decision, err, fixture.core.restoreCalls, fixture.provider.reservation.State)
		}
	})
}

func TestRestartOperationPlanMismatchRetainsProviderAuthority(t *testing.T) {
	fixture := newWorkflowFixture(t)
	prepared := fixture.prepare(t)
	lock, _ := fixture.repo.OperationByID(context.Background(), prepared.Operation.OperationID)
	lock.PlanRevision = strings.Repeat("0", 64)
	decision, err := fixture.workflow.Reconcile(context.Background(), lock)
	if err != nil || decision.State != protectionoperations.StateReconcileRequired || fixture.provider.reservation.State != neutralfallback.ReservationReserved || countEvent(*fixture.events, "provider:release") != 0 {
		t.Fatalf("decision=%#v err=%v reservation=%s events=%v", decision, err, fixture.provider.reservation.State, *fixture.events)
	}
}

func TestMirrorDeletionCannotReleaseProviderAndApplyFailsClosed(t *testing.T) {
	fixture := newWorkflowFixture(t)
	prepared := fixture.prepare(t)
	if err := fixture.db.Where("operation_id = ?", prepared.Operation.OperationID).Delete(&repository.FallbackTargetLeaseModel{}).Error; err != nil {
		t.Fatal(err)
	}
	_, err := fixture.workflow.Apply(context.Background(), ApplyWorkflowRequestV1{
		Actor: "admin", OperationID: prepared.Operation.OperationID, OperationRevision: prepared.Operation.Revision,
		PlanDigest: fixture.plan.PlanDigest, ProviderReservationRevision: prepared.Operation.ProviderReservationRevision,
		ExpectedState: domain.NativeActualPrepared, Confirmed: true,
	})
	if err == nil || fixture.provider.reservation.State != neutralfallback.ReservationReserved || countEvent(*fixture.events, "provider:release") != 0 {
		t.Fatalf("err=%v reservation=%s events=%v", err, fixture.provider.reservation.State, *fixture.events)
	}
}

func assertEventOrder(t *testing.T, events []string, expected ...string) {
	t.Helper()
	position := -1
	for _, want := range expected {
		found := -1
		for index := position + 1; index < len(events); index++ {
			if events[index] == want {
				found = index
				break
			}
		}
		if found < 0 {
			t.Fatalf("event %q not found after %d in %v", want, position, events)
		}
		position = found
	}
}

func countEvent(events []string, target string) int {
	count := 0
	for _, event := range events {
		if event == target {
			count++
		}
	}
	return count
}
