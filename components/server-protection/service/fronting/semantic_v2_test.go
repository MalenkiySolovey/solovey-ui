package fronting

import (
	"context"
	"errors"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

type semanticFixtureSourceV2 struct {
	input FrontingPlanInputV2
	plan  FrontingStrategyPlanV2
}

func (s semanticFixtureSourceV2) ResourcesV2(context.Context, time.Time) ([]SemanticResourceV2, error) {
	return []SemanticResourceV2{{ResourceID: s.input.Socket.ResourceID, DisplayIdentity: "Fixture public", CurrentConfigurationRevision: s.input.Socket.CurrentConfigurationRevision,
		Runtime: s.input.Runtime, SocketClaims: []FrontingSocketClaimV1{s.input.Socket}, BackendReferences: append([]hostresources.FrontingBackendReferenceV1(nil), s.input.BackendReferences...)}}, nil
}
func (s semanticFixtureSourceV2) ResolvePreviewV2(context.Context, FrontingPreviewRequestV2, SelectorSetV1, time.Time) (FrontingPlanInputV2, error) {
	return s.input, nil
}
func (s semanticFixtureSourceV2) ResolvePrepareV2(context.Context, FrontingPrepareRequestV2, time.Time) (FrontingStrategyPlanV2, error) {
	return s.plan, nil
}

func TestSemanticPreviewIsReadOnlyAndRejectsALPN(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	source := semanticFixtureSourceV2{input: fixture.source.input, plan: fixture.plan}
	service := &SemanticServiceV2{Workflow: fixture.workflow, Repository: fixture.repository, Source: source, Now: func() time.Time { return fixture.now }}
	request := semanticPreviewRequestV2(source.input)
	before, err := fixture.manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.Preview(context.Background(), request)
	if err != nil || plan.CanonicalPlanDigest != fixture.plan.CanonicalPlanDigest || plan.Strategy.Actual != FrontingActualNotAppliedV2 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	after, err := fixture.manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fixture.provider.mu.Lock()
	calls := append([]string(nil), fixture.provider.calls...)
	fixture.provider.mu.Unlock()
	if len(before) != 0 || len(after) != 0 || len(calls) != 0 || fixture.nginx.Reloads != 0 {
		t.Fatalf("preview mutated: before=%d after=%d provider=%#v reloads=%d", len(before), len(after), calls, fixture.nginx.Reloads)
	}
	request.Selectors = []SelectorRouteInputV1{{SNI: "panel.example", ALPN: []string{"h2"}, TargetReferenceRevision: request.BackendReferences[0].CanonicalReferenceRevision}}
	_, err = service.Preview(context.Background(), request)
	var semantic *SemanticErrorV2
	if !errors.As(err, &semantic) || semantic.Code != "alpn_routing_unsupported" {
		t.Fatalf("ALPN error=%v", err)
	}
}

func TestSemanticPrepareApplyRollbackAreExplicitFencedAndIdempotent(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	source := semanticFixtureSourceV2{input: fixture.source.input, plan: fixture.plan}
	service := &SemanticServiceV2{Workflow: fixture.workflow, Repository: fixture.repository, Source: source, Now: func() time.Time { return fixture.now }}
	prepareRequest := FrontingPrepareRequestV2{
		PlanID: fixture.plan.PlanID, PlanDigest: fixture.plan.CanonicalPlanDigest, ResourceID: fixture.plan.PublicSocket.ResourceID,
		RuntimeIdentityRevision: fixture.plan.Runtime.IdentityRevision, StrategyCapabilityRevision: fixture.plan.StrategyCapabilityRevision,
		SocketClaimRevision: fixture.plan.PublicSocket.ClaimRevision, SelectorSetRevision: fixture.plan.Selectors.SelectorSetRevision,
		TargetReferenceRevisions: append([]string(nil), fixture.plan.Targets.ReferenceRevisions...), IdempotencyKey: "semantic-prepare",
		ExperimentalRiskAcknowledged: true, Acknowledgement: "PREPARE FRONTING " + fixture.plan.CanonicalPlanDigest,
	}
	prepared, err := service.Prepare(context.Background(), prepareRequest, "tester")
	if err != nil || prepared.ActualState != "PREPARED" || fixture.nginx.Reloads != 0 {
		t.Fatalf("prepared=%#v reloads=%d err=%v", prepared, fixture.nginx.Reloads, err)
	}
	joined, err := service.Prepare(context.Background(), prepareRequest, "tester")
	if err != nil || joined.OperationID != prepared.OperationID || fixture.nginx.Reloads != 0 {
		t.Fatalf("joined=%#v err=%v", joined, err)
	}

	applyRequest := FrontingApplyRequestV2{OperationID: prepared.OperationID, OperationRevision: prepared.OperationRevision, PlanDigest: prepared.PlanDigest,
		TargetAuthorityRevisions: authorityRevisionsFromViewV2(prepared), IdempotencyKey: "semantic-apply", Confirmation: "APPLY FRONTING " + prepared.OperationID}
	applied, err := service.Apply(context.Background(), applyRequest)
	if err != nil || applied.ActualState != "APPLIED" || fixture.nginx.Reloads != 1 {
		t.Fatalf("applied=%#v reloads=%d err=%v", applied, fixture.nginx.Reloads, err)
	}
	status, err := service.Status(context.Background())
	if err != nil || len(status.Items) != 1 || status.Items[0].ActualState != "RECONCILE_REQUIRED" || status.Items[0].RecoveryState != "CURRENT_RUNTIME_UNVERIFIED" {
		t.Fatalf("historical applied status=%#v err=%v", status, err)
	}
	replayed, err := service.Apply(context.Background(), applyRequest)
	if err != nil || replayed.OperationID != applied.OperationID || replayed.OperationRevision != applied.OperationRevision || fixture.nginx.Reloads != 1 {
		t.Fatalf("replay=%#v reloads=%d err=%v", replayed, fixture.nginx.Reloads, err)
	}
	conflict := applyRequest
	conflict.PlanDigest = stringReplaceLastV2(conflict.PlanDigest)
	_, err = service.Apply(context.Background(), conflict)
	var semantic *SemanticErrorV2
	if !errors.As(err, &semantic) || semantic.Code != "operation_conflict" {
		t.Fatalf("conflict error=%v", err)
	}

	rollbackRequest := FrontingRollbackRequestV2{OperationID: applied.OperationID, OperationRevision: applied.OperationRevision, IdempotencyKey: "semantic-rollback", Confirmation: "ROLLBACK FRONTING " + applied.OperationID}
	rolled, err := service.Rollback(context.Background(), rollbackRequest)
	if err != nil || rolled.ActualState != "ROLLED_BACK" || fixture.nginx.Reloads != 2 {
		t.Fatalf("rolled=%#v reloads=%d err=%v", rolled, fixture.nginx.Reloads, err)
	}
	replayedRollback, err := service.Rollback(context.Background(), rollbackRequest)
	if err != nil || replayedRollback.OperationRevision != rolled.OperationRevision || fixture.nginx.Reloads != 2 {
		t.Fatalf("rollback replay=%#v err=%v", replayedRollback, err)
	}
}

func TestSemanticPersistedRowsAreValidatedBeforeProjection(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	source := semanticFixtureSourceV2{input: fixture.source.input, plan: fixture.plan}
	service := &SemanticServiceV2{Workflow: fixture.workflow, Repository: fixture.repository, Source: source, Now: func() time.Time { return fixture.now }}
	request := FrontingPrepareRequestV2{
		PlanID: fixture.plan.PlanID, PlanDigest: fixture.plan.CanonicalPlanDigest, ResourceID: fixture.plan.PublicSocket.ResourceID,
		RuntimeIdentityRevision: fixture.plan.Runtime.IdentityRevision, StrategyCapabilityRevision: fixture.plan.StrategyCapabilityRevision,
		SocketClaimRevision: fixture.plan.PublicSocket.ClaimRevision, SelectorSetRevision: fixture.plan.Selectors.SelectorSetRevision,
		TargetReferenceRevisions: append([]string(nil), fixture.plan.Targets.ReferenceRevisions...), IdempotencyKey: "semantic-persisted-validation",
		ExperimentalRiskAcknowledged: true, Acknowledgement: "PREPARE FRONTING " + fixture.plan.CanonicalPlanDigest,
	}
	if _, err := service.Prepare(context.Background(), request, "tester"); err != nil {
		t.Fatal(err)
	}
	state, err := fixture.repository.FrontingStateV2(context.Background(), fixture.plan.PublicSocket.ResourceID)
	if err != nil || !validPersistedFrontingStateV2(state) {
		t.Fatalf("valid state rejected=%#v err=%v", state, err)
	}
	state.LeaseMirrorsJSON = []byte(`[{"kind":"BACKEND","state":"ACTIVE","authorityRevision":"not-a-revision"}]`)
	if validPersistedFrontingStateV2(state) {
		t.Fatal("invalid lease mirror was trusted")
	}
	state = protectionrepository.FrontingStateV2Model{ResourceID: "broken", Schema: "broken", DesiredStrategy: "DISABLED", ActualState: "APPLIED"}
	if validPersistedFrontingStateV2(state) {
		t.Fatal("invalid persisted row was trusted")
	}
}

func TestSemanticOperationLegacyAndRecoveryInspectionDoNotMutate(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	source := semanticFixtureSourceV2{input: fixture.source.input, plan: fixture.plan}
	service := &SemanticServiceV2{Workflow: fixture.workflow, Repository: fixture.repository, Source: source, Now: func() time.Time { return fixture.now }}
	prepared := fixture.prepare(t, "semantic-inspection")
	fixture.provider.mu.Lock()
	beforeCalls := len(fixture.provider.calls)
	fixture.provider.mu.Unlock()
	view, err := service.Operation(context.Background(), prepared.OperationID)
	if err != nil || view.ActualState != "PREPARED" || view.CompatibilityState != "CURRENT_V2" {
		t.Fatalf("view=%#v err=%v", view, err)
	}
	recovery, err := service.Recovery(context.Background(), prepared.OperationID)
	if err != nil || recovery.PermittedNextAction != "APPLY_OR_ROLLBACK" {
		t.Fatalf("recovery=%#v err=%v", recovery, err)
	}
	fixture.provider.mu.Lock()
	afterCalls := len(fixture.provider.calls)
	fixture.provider.mu.Unlock()
	if beforeCalls != afterCalls || fixture.nginx.Reloads != 0 {
		t.Fatalf("inspection mutated provider=%d->%d reloads=%d", beforeCalls, afterCalls, fixture.nginx.Reloads)
	}
}

func semanticPreviewRequestV2(input FrontingPlanInputV2) FrontingPreviewRequestV2 {
	return FrontingPreviewRequestV2{ResourceID: input.Socket.ResourceID, ExpectedCurrentConfigurationRevision: input.Socket.CurrentConfigurationRevision,
		RequestedStrategy: input.DesiredStrategy, SocketClaim: FrontingSocketClaimReferenceV2{ResourceID: input.Socket.ResourceID, EndpointID: input.Socket.EndpointID, ClaimRevision: input.Socket.ClaimRevision},
		BackendReferences: append([]hostresources.FrontingBackendReferenceV1(nil), input.BackendReferences...), SelectedProxyMode: input.ProxyMode, Selectors: []SelectorRouteInputV1{}, Default: input.Selectors.Default}
}

func authorityRevisionsFromViewV2(value FrontingOperationViewV2) []string {
	result := make([]string, 0, len(value.Leases))
	for _, lease := range value.Leases {
		result = append(result, lease.AuthorityRevision)
	}
	return result
}

func stringReplaceLastV2(value string) string {
	if value[len(value)-1] == 'a' {
		return value[:len(value)-1] + "b"
	}
	return value[:len(value)-1] + "a"
}
