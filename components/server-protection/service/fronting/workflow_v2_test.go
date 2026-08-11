package fronting

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
)

type fixedPlanSourceV2 struct {
	mu    sync.Mutex
	input FrontingPlanInputV2
	err   error
}

type cancelAfterHelperOperationV2 struct {
	base   Helper
	mutate protectionhelper.Operation
	cancel context.CancelFunc
}

type helperFuncV2 func(context.Context, protectionhelper.Request) (protectionhelper.Response, error)

func (fn helperFuncV2) Execute(ctx context.Context, request protectionhelper.Request) (protectionhelper.Response, error) {
	return fn(ctx, request)
}

func (h cancelAfterHelperOperationV2) Execute(ctx context.Context, request protectionhelper.Request) (protectionhelper.Response, error) {
	response, err := h.base.Execute(ctx, request)
	if request.Operation == h.mutate {
		h.cancel()
		return response, context.Canceled
	}
	return response, err
}

func (s *fixedPlanSourceV2) CurrentFrontingPlanInputV2(context.Context, FrontingStrategyPlanV2) (FrontingPlanInputV2, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.input, s.err
}

type memoryEndpointLeaseProviderV2 struct {
	mu                        sync.Mutex
	id                        string
	now                       time.Time
	lease                     hostresources.EndpointLeaseV1
	calls                     []string
	fail                      map[string]bool
	acquireRequest            hostresources.AcquireEndpointLeaseRequestV1
	beforeAcquire             func()
	acquireCommittedThenError bool
}

func (p *memoryEndpointLeaseProviderV2) ProviderID() string {
	if p.id != "" {
		return p.id
	}
	return "provider"
}

func (p *memoryEndpointLeaseProviderV2) AcquireEndpointLease(_ context.Context, request hostresources.AcquireEndpointLeaseRequestV1) (hostresources.EndpointLeaseV1, error) {
	if p.beforeAcquire != nil {
		p.beforeAcquire()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, "acquire")
	p.acquireRequest = request
	if p.fail["acquire"] {
		return hostresources.EndpointLeaseV1{}, hostresources.ErrEndpointLeaseConflictV1
	}
	if p.lease.LeaseID != "" {
		return p.lease, nil
	}
	p.lease, _ = hostresources.FinalizeEndpointLeaseV1(hostresources.EndpointLeaseV1{LeaseID: "endpoint-lease-" + p.ProviderID(), AuthorityProviderID: p.ProviderID(), HolderID: request.HolderID,
		ExactReference: request.ExactReference, State: hostresources.EndpointLeaseReserved, IssuedAt: p.now.Unix(), RenewedAt: p.now.Unix(), ExpiresAt: p.now.Add(10 * time.Minute).Unix()})
	if p.acquireCommittedThenError {
		return hostresources.EndpointLeaseV1{}, errors.New("provider result unavailable")
	}
	return p.lease, nil
}

func (p *memoryEndpointLeaseProviderV2) FenceEndpointLease(_ context.Context, request hostresources.MutateEndpointLeaseRequestV1) (hostresources.EndpointLeaseV1, error) {
	return p.mutate("fence", request, hostresources.EndpointLeaseMutationPending)
}

func (p *memoryEndpointLeaseProviderV2) ActivateEndpointLease(_ context.Context, request hostresources.MutateEndpointLeaseRequestV1) (hostresources.EndpointLeaseV1, error) {
	return p.mutate("activate", request, hostresources.EndpointLeaseActive)
}

func (p *memoryEndpointLeaseProviderV2) mutate(call string, request hostresources.MutateEndpointLeaseRequestV1, state hostresources.EndpointLeaseState) (hostresources.EndpointLeaseV1, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, call)
	if p.fail[call] || request.LeaseID != p.lease.LeaseID || request.ExpectedRevision != p.lease.LeaseRevision {
		return hostresources.EndpointLeaseV1{}, errors.New("secret provider failure")
	}
	next := p.lease
	next.State = state
	if state == hostresources.EndpointLeaseActive {
		next.RenewedAt++
		next.ExpiresAt++
	}
	p.lease, _ = hostresources.FinalizeEndpointLeaseV1(next)
	return p.lease, nil
}

func (p *memoryEndpointLeaseProviderV2) ReleaseEndpointLease(_ context.Context, request hostresources.ReleaseEndpointLeaseRequestV1) (hostresources.EndpointLeaseV1, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, "release")
	if p.fail["release"] || request.LeaseID != p.lease.LeaseID || request.ExpectedRevision != p.lease.LeaseRevision {
		return hostresources.EndpointLeaseV1{}, errors.New("secret provider failure")
	}
	next := p.lease
	next.State, next.ReleasedAt = hostresources.EndpointLeaseReleased, max(next.RenewedAt, p.now.Unix())
	p.lease, _ = hostresources.FinalizeEndpointLeaseV1(next)
	return p.lease, nil
}

func (p *memoryEndpointLeaseProviderV2) GetEndpointLease(context.Context, hostresources.GetEndpointLeaseRequestV1) (hostresources.EndpointLeaseV1, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, "get")
	if p.fail["get"] {
		return hostresources.EndpointLeaseV1{}, errors.New("secret provider failure")
	}
	return p.lease, nil
}

func (p *memoryEndpointLeaseProviderV2) ListEndpointLeases(context.Context, hostresources.ListEndpointLeasesRequestV1) ([]hostresources.EndpointLeaseV1, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lease.LeaseID == "" {
		return nil, nil
	}
	return []hostresources.EndpointLeaseV1{p.lease}, nil
}

type memoryLeaseDirectoryV2 struct {
	provider *memoryEndpointLeaseProviderV2
}

func (d memoryLeaseDirectoryV2) EndpointLeaseProviderV1(id string) (hostresources.EndpointLeaseProviderV1, bool) {
	return d.provider, id == d.provider.ProviderID()
}

func (d memoryLeaseDirectoryV2) EndpointLeasesByHolderV1(ctx context.Context, holder string) ([]hostresources.EndpointLeaseV1, error) {
	return d.provider.ListEndpointLeases(ctx, hostresources.ListEndpointLeasesRequestV1{HolderID: holder, Limit: 16})
}

type providerIDOverrideV2 struct {
	hostresources.EndpointLeaseProviderV1
	id string
}

func (p providerIDOverrideV2) ProviderID() string { return p.id }

type fixedLeaseDirectoryV2 struct {
	provider hostresources.EndpointLeaseProviderV1
}

func (d fixedLeaseDirectoryV2) EndpointLeaseProviderV1(string) (hostresources.EndpointLeaseProviderV1, bool) {
	return d.provider, d.provider != nil
}

func (d fixedLeaseDirectoryV2) EndpointLeasesByHolderV1(ctx context.Context, holder string) ([]hostresources.EndpointLeaseV1, error) {
	return d.provider.ListEndpointLeases(ctx, hostresources.ListEndpointLeasesRequestV1{HolderID: holder, Limit: 16})
}

type workflowV2Fixture struct {
	frontingFixture
	now         time.Time
	plan        FrontingStrategyPlanV2
	source      *fixedPlanSourceV2
	provider    *memoryEndpointLeaseProviderV2
	healthCalls int
	healthLease hostresources.EndpointLeaseState
}

func newWorkflowV2Fixture(t *testing.T, proxy hostresources.ProxyMode) *workflowV2Fixture {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	input := l4PlanInputV2(t, now, false)
	if proxy == hostresources.ProxyModeOn {
		input.Inventory[0].AcceptsProxyProtocol = hostresources.CapabilityYes
		input.BackendReferences[0], _ = hostresources.ReferenceFrontingBackendV1(input.Inventory[0], proxy, now)
		input.ProxyMode = proxy
	}
	plan, err := PlanFrontingStrategyV2(input)
	if err != nil || len(plan.Safety.Blocks) != 0 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	base := newFrontingFixture(t, passingFrontingHealth)
	provider := &memoryEndpointLeaseProviderV2{now: now, fail: map[string]bool{}}
	source := &fixedPlanSourceV2{input: input}
	fixture := &workflowV2Fixture{frontingFixture: base, now: now, plan: plan, source: source, provider: provider}
	base.workflow.Now = func() time.Time { return fixture.now }
	base.workflow.V2Plans = source
	base.workflow.V2Leases = memoryLeaseDirectoryV2{provider: provider}
	base.workflow.V2Artifacts = base.storage
	base.workflow.V2Health = func(_ context.Context, request FixedL4HealthRequestV2) (FixedL4HealthEvidenceV2, error) {
		fixture.healthCalls++
		provider.mu.Lock()
		if fixture.healthLease == "" {
			fixture.healthLease = provider.lease.State
		}
		provider.mu.Unlock()
		return FixedL4HealthEvidenceV2{Schema: FixedL4HealthSchemaV2, OperationID: request.OperationID, OperationRevision: request.OperationRevision,
			PlanDigest: request.PlanDigest, CandidateRevision: request.CandidateRevision, CandidateSHA256: request.CandidateSHA256,
			SocketClaimRevision: request.SocketClaimRevision, BackendReferenceRevision: request.BackendReferenceRevision, LeaseRevision: request.LeaseRevision,
			ProxyMode: request.ProxyMode, PublicFixtureAccepted: true, ExpectedBackendReached: true, BackendIdentityMarker: request.BackendReferenceRevision,
			ProxyHeaderObserved: request.ProxyMode == hostresources.ProxyModeOn, ObservedAt: fixture.now.Unix(), ExpiresAt: fixture.now.Add(20 * time.Second).Unix()}, nil
	}
	return fixture
}

func (f *workflowV2Fixture) prepare(t *testing.T, key string) WorkflowResultV2 {
	t.Helper()
	result, err := f.workflow.PrepareV2(context.Background(), PrepareV2Input{Plan: f.plan, Actor: "tester", IdempotencyKey: key,
		Confirmation: "PREPARE FRONTING " + f.plan.CanonicalPlanDigest})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestFixedL4CandidateV2GoldenShapeAndForbiddenGrammar(t *testing.T) {
	revisions := map[hostresources.ProxyMode]string{}
	for _, mode := range []hostresources.ProxyMode{hostresources.ProxyModeOff, hostresources.ProxyModeOn} {
		t.Run(string(mode), func(t *testing.T) {
			fixture := newWorkflowV2Fixture(t, mode)
			candidate, err := RenderFixedL4CandidateV2(fixture.plan, fixture.source.input.Inventory[0], fixture.now)
			if err != nil || len(candidate.Bytes) > MaxFutureCandidateBytesV1 || strings.Count(string(candidate.Bytes), "upstream ") != 1 ||
				strings.Count(string(candidate.Bytes), "proxy_pass ") != 1 || strings.Contains(string(candidate.Bytes), "ssl_preread") ||
				strings.Contains(string(candidate.Bytes), "resolver") || strings.Contains(string(candidate.Bytes), "$") ||
				strings.Contains(string(candidate.Bytes), "proxy_protocol on;") != (mode == hostresources.ProxyModeOn) {
				t.Fatalf("candidate=%s err=%v", candidate.Bytes, err)
			}
			again, _ := RenderFixedL4CandidateV2(fixture.plan, fixture.source.input.Inventory[0], fixture.now)
			if candidate.Revision != again.Revision || candidate.SHA256 != again.SHA256 || string(candidate.Bytes) != string(again.Bytes) {
				t.Fatal("fixed L4 candidate is not deterministic")
			}
			revisions[mode] = candidate.Revision
		})
	}
	if revisions[hostresources.ProxyModeOff] == revisions[hostresources.ProxyModeOn] {
		t.Fatal("selected PROXY mode did not change candidate revision")
	}
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	candidate, err := RenderFixedL4CandidateV2(fixture.plan, fixture.source.input.Inventory[0], fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Bytes = []byte(strings.Repeat("x", MaxFutureCandidateBytesV1+1))
	candidate.SHA256 = fixedL4DigestV2(candidate.Bytes)
	if err := ValidateFixedL4CandidateV2(candidate, fixture.plan); err == nil || err.Error() != "candidate_too_large" {
		t.Fatalf("oversized candidate error=%v", err)
	}
}

func TestFixedL4CandidateV2IPv6GoldenHasExplicitSingleFamily(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	input := l4PlanInputV2(t, now, false)
	fact := backendFactAddressV2(t, now, "::1", hostresources.AddressFamilyIPv6)
	reference, err := hostresources.ReferenceFrontingBackendV1(fact, hostresources.ProxyModeOff, now)
	if err != nil {
		t.Fatal(err)
	}
	input.Inventory = []hostresources.FrontingBackendFactV1{fact}
	input.BackendReferences = []hostresources.FrontingBackendReferenceV1{reference}
	input.Socket, err = FinalizeFrontingSocketClaimV1(socketFixtureV1(now, "::", hostresources.AddressFamilyIPv6, true))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanFrontingStrategyV2(input)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := RenderFixedL4CandidateV2(plan, fact, now)
	if err != nil {
		t.Fatal(err)
	}
	text := string(candidate.Bytes)
	if !strings.Contains(text, "listen [::]:443;") || !strings.Contains(text, "server [::1]:10001;") || strings.Contains(text, "0.0.0.0") ||
		strings.Count(text, "  listen ") != 1 || candidate.Listener.Address != "::" || candidate.Backend.AddressFamily != hostresources.AddressFamilyIPv6 {
		t.Fatalf("IPv6 candidate did not preserve explicit family semantics:\n%s", text)
	}
}

func TestWorkflowV2PersistsOperationBeforeExactLeaseRequest(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	persisted := false
	fixture.provider.beforeAcquire = func() {
		items, err := fixture.manager.List(context.Background())
		if err == nil && len(items) == 1 && items[0].Kind == protectionoperations.KindFronting && items[0].State == protectionoperations.StatePrepared {
			persisted = true
		}
	}
	prepared := fixture.prepare(t, "v2-persist-before-lease")
	fixture.provider.mu.Lock()
	request := fixture.provider.acquireRequest
	fixture.provider.mu.Unlock()
	if !persisted || request.HolderID != prepared.OperationID || request.Purpose != hostresources.EndpointLeasePurposeL4FrontingV1 ||
		request.ExactReference != fixture.plan.Targets.BackendReferences[0] || request.FreshnessSeconds == 0 {
		t.Fatalf("persisted=%v request=%#v prepared=%#v", persisted, request, prepared)
	}
}

func TestWorkflowV2RejectsNonFixedStrategiesAndSelectorShapesBeforeOperation(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	referenceRevision := fixture.plan.Targets.BackendReferences[0].CanonicalReferenceRevision
	selector, err := CanonicalizeSelectorSetV1([]SelectorRouteInputV1{{SNI: "route.example", TargetReferenceRevision: referenceRevision}}, SelectorDefaultV1{})
	if err != nil {
		t.Fatal(err)
	}
	fixedDefault, err := CanonicalizeSelectorSetV1(nil, SelectorDefaultV1{Policy: SelectorDefaultFixedSafe, TargetReferenceRevision: referenceRevision})
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(FrontingStrategyPlanV2) FrontingStrategyPlanV2{
		"sni": func(plan FrontingStrategyPlanV2) FrontingStrategyPlanV2 {
			plan.Strategy.Desired, plan.Strategy.Selected = StrategySNIPreread, StrategySNIPreread
			plan.Safety.Projection = plan.Strategy
			return rehashPlanV2(plan)
		},
		"http": func(plan FrontingStrategyPlanV2) FrontingStrategyPlanV2 {
			plan.Strategy.Desired, plan.Strategy.Selected = StrategyHTTPTerminating, StrategyHTTPTerminating
			plan.Safety.Projection = plan.Strategy
			return rehashPlanV2(plan)
		},
		"udp": func(plan FrontingStrategyPlanV2) FrontingStrategyPlanV2 {
			plan.Strategy.Desired, plan.Strategy.Selected = StrategyUDPQUIC, StrategyUDPQUIC
			plan.Safety.Projection = plan.Strategy
			return rehashPlanV2(plan)
		},
		"selector": func(plan FrontingStrategyPlanV2) FrontingStrategyPlanV2 {
			plan.Selectors = selector
			return rehashPlanV2(plan)
		},
		"fixed_default": func(plan FrontingStrategyPlanV2) FrontingStrategyPlanV2 {
			plan.Selectors = fixedDefault
			return rehashPlanV2(plan)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			plan := mutate(fixture.plan)
			_, err := fixture.workflow.PrepareV2(context.Background(), PrepareV2Input{Plan: plan, Actor: "tester", IdempotencyKey: "v2-reject-" + name,
				Confirmation: "PREPARE FRONTING " + plan.CanonicalPlanDigest})
			if err == nil || err.Error() != "candidate_invalid" {
				t.Fatalf("error=%v plan=%#v", err, plan.Strategy)
			}
		})
	}
	items, err := fixture.manager.List(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatalf("rejected shapes persisted operations: %#v err=%v", items, err)
	}
}

func TestWorkflowV2ConflictingPrepareReplayFailsClosed(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	fixture.prepare(t, "v2-conflicting-prepare")
	input := fixture.source.input
	input.Now = fixture.now.Add(time.Second)
	other, err := PlanFrontingStrategyV2(input)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.workflow.PrepareV2(context.Background(), PrepareV2Input{Plan: other, Actor: "tester", IdempotencyKey: "v2-conflicting-prepare",
		Confirmation: "PREPARE FRONTING " + other.CanonicalPlanDigest})
	if err == nil || err.Error() != "plan_stale" {
		t.Fatalf("conflicting replay error=%v", err)
	}
	fixture.provider.mu.Lock()
	mutations := filterLeaseMutationsV2(fixture.provider.calls)
	fixture.provider.mu.Unlock()
	if strings.Join(mutations, ",") != "acquire" {
		t.Fatalf("conflicting replay changed provider authority: %v", mutations)
	}
}

func TestWorkflowV2DefinitiveLeaseConflictCancelsWithoutArtifactOrMutation(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	fixture.provider.fail["acquire"] = true
	result, err := fixture.workflow.PrepareV2(context.Background(), PrepareV2Input{Plan: fixture.plan, Actor: "tester", IdempotencyKey: "v2-lease-conflict",
		Confirmation: "PREPARE FRONTING " + fixture.plan.CanonicalPlanDigest})
	if err == nil || err.Error() != "lease_conflict" || result.State != protectionoperations.StateCancelled || result.LeaseID != "" ||
		countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != 0 || fixture.nginx.Reloads != 0 {
		t.Fatalf("result=%#v calls=%v err=%v", result, fixture.nginx.Calls, err)
	}
	if _, loadErr := fixture.workflow.loadV2(result.OperationID); loadErr != nil {
		t.Fatalf("cancelled v2 checkpoint is not readable: %v", loadErr)
	}
}

func TestWorkflowV2RejectsMismatchedProviderAuthorityBeforeAcquire(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	fixture.workflow.V2Leases = fixedLeaseDirectoryV2{provider: providerIDOverrideV2{
		EndpointLeaseProviderV1: fixture.provider,
		id:                      "different-provider",
	}}
	result, err := fixture.workflow.PrepareV2(context.Background(), PrepareV2Input{Plan: fixture.plan, Actor: "tester", IdempotencyKey: "v2-provider-id-mismatch",
		Confirmation: "PREPARE FRONTING " + fixture.plan.CanonicalPlanDigest})
	fixture.provider.mu.Lock()
	calls := append([]string(nil), fixture.provider.calls...)
	fixture.provider.mu.Unlock()
	if err == nil || err.Error() != "lease_conflict" || result.State != protectionoperations.StateCancelled || result.LeaseID != "" || len(calls) != 0 {
		t.Fatalf("result=%#v calls=%v err=%v", result, calls, err)
	}
}

func TestSafeLeaseCallV2HonorsCallerDeadlineWhenProviderIgnoresContext(t *testing.T) {
	release := make(chan struct{})
	providerReturned := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	type result struct {
		err error
	}
	done := make(chan result, 1)
	go func() {
		_, err := safeLeaseCallV2(ctx, func(context.Context) (hostresources.EndpointLeaseV1, error) {
			<-release
			close(providerReturned)
			return hostresources.EndpointLeaseV1{}, nil
		})
		done <- result{err: err}
	}()
	select {
	case value := <-done:
		close(release)
		<-providerReturned
		if value.err == nil || value.err.Error() != "lease_provider_unavailable" {
			t.Fatalf("bounded provider error=%v", value.err)
		}
	case <-time.After(250 * time.Millisecond):
		close(release)
		<-providerReturned
		<-done
		t.Fatal("provider call ignored the caller deadline")
	}
}

func TestWorkflowV2NeverInterpretsV1CheckpointAsV2Evidence(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	operationID := "operation-11111111111111111111111111111111"
	if err := fixture.storage.WriteFrontingState(operationID, []byte("{\"version\":1,\"operationId\":\""+operationID+"\"}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.workflow.loadV2(operationID); err == nil {
		t.Fatal("v1 checkpoint became v2 workflow evidence")
	}
}

func TestWorkflowV2PrepareApplyOrderAndIdempotency(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	prepared := fixture.prepare(t, "v2-happy")
	if prepared.State != protectionoperations.StatePrepared || prepared.LeaseState != hostresources.EndpointLeaseReserved || fixture.nginx.Reloads != 0 || countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != 0 {
		t.Fatalf("prepared=%#v reloads=%d calls=%v", prepared, fixture.nginx.Reloads, fixture.nginx.Calls)
	}
	result, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err != nil || result.State != protectionoperations.StateApplied || result.LeaseState != hostresources.EndpointLeaseActive || fixture.nginx.Reloads != 1 || countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != 1 || fixture.healthCalls != 1 {
		t.Fatalf("result=%#v reloads=%d calls=%v health=%d err=%v", result, fixture.nginx.Reloads, fixture.nginx.Calls, fixture.healthCalls, err)
	}
	replayed, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err != nil || replayed.State != protectionoperations.StateApplied || fixture.nginx.Reloads != 1 || countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != 1 || fixture.healthCalls != 2 {
		t.Fatalf("replay=%#v reloads=%d calls=%v health=%d err=%v", replayed, fixture.nginx.Reloads, fixture.nginx.Calls, fixture.healthCalls, err)
	}
	fixture.provider.mu.Lock()
	calls := append([]string(nil), fixture.provider.calls...)
	fixture.provider.mu.Unlock()
	if strings.Join(filterLeaseMutationsV2(calls), ",") != "acquire,fence,activate" {
		t.Fatalf("lease mutation order=%v", calls)
	}
	if fixture.healthLease != hostresources.EndpointLeaseMutationPending {
		t.Fatalf("health ran outside the provider mutation fence: %s", fixture.healthLease)
	}
	wantEngineOrder := strings.Join([]string{string(protectionhelper.OperationNginxValidate), string(protectionhelper.OperationNginxInstall),
		string(protectionhelper.OperationNginxSwitch), string(protectionhelper.OperationNginxReload), string(protectionhelper.OperationNginxVerify)}, ",")
	if got := strings.Join(filterNginxMutationsV2(fixture.nginx.Calls), ","); got != wantEngineOrder {
		t.Fatalf("managed engine order=%s calls=%v", got, fixture.nginx.Calls)
	}
}

func TestWorkflowV2ValidationAndHealthFailuresRollbackOnce(t *testing.T) {
	for _, stage := range []string{"validation", "health", "health_panic"} {
		t.Run(stage, func(t *testing.T) {
			fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
			prepared := fixture.prepare(t, "v2-failure-"+stage)
			if stage == "validation" {
				fixture.nginx.Fail[protectionhelper.OperationNginxValidate] = errors.New("raw helper detail")
			} else if stage == "health" {
				fixture.workflow.V2Health = func(context.Context, FixedL4HealthRequestV2) (FixedL4HealthEvidenceV2, error) {
					return FixedL4HealthEvidenceV2{}, errors.New("raw backend detail")
				}
			} else {
				fixture.workflow.V2Health = func(context.Context, FixedL4HealthRequestV2) (FixedL4HealthEvidenceV2, error) {
					panic("raw health panic detail")
				}
			}
			result, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
				Confirmation: "APPLY FRONTING " + prepared.OperationID})
			if err == nil || result.State != protectionoperations.StateRolledBack || result.LeaseState != hostresources.EndpointLeaseReleased ||
				strings.Contains(err.Error(), "raw") || fixture.nginx.ActiveRevision != strings.Repeat("a", 64) {
				t.Fatalf("result=%#v active=%s err=%v", result, fixture.nginx.ActiveRevision, err)
			}
			checkpoint, loadErr := fixture.workflow.loadV2(prepared.OperationID)
			if loadErr != nil || checkpoint.RollbackAttemptCount != 1 || !checkpoint.Detached {
				t.Fatalf("checkpoint=%#v err=%v", checkpoint, loadErr)
			}
		})
	}
}

func TestWorkflowV2ProxyHealthMismatchUsesTypedFailureAndRollsBack(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOn)
	prepared := fixture.prepare(t, "v2-proxy-health-mismatch")
	good := fixture.workflow.V2Health
	fixture.workflow.V2Health = func(ctx context.Context, request FixedL4HealthRequestV2) (FixedL4HealthEvidenceV2, error) {
		evidence, err := good(ctx, request)
		evidence.ProxyHeaderObserved = false
		return evidence, err
	}
	result, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err == nil || err.Error() != "proxy_protocol_mismatch" || result.State != protectionoperations.StateRolledBack || result.LeaseState != hostresources.EndpointLeaseReleased {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestWorkflowV2CancellationBeforeMarkerReleasesLeaseWithoutMutation(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	prepared := fixture.prepare(t, "v2-cancel-before-marker")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := fixture.workflow.ApplyV2(ctx, ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err == nil || result.State != protectionoperations.StateCancelled || result.LeaseState != hostresources.EndpointLeaseReleased ||
		countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != 0 || fixture.nginx.Reloads != 0 {
		t.Fatalf("result=%#v calls=%v reloads=%d err=%v", result, fixture.nginx.Calls, fixture.nginx.Reloads, err)
	}
}

func TestWorkflowV2CancellationAfterReloadRetainsAuthorityForRestartDecision(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	prepared := fixture.prepare(t, "v2-cancel-after-reload")
	ctx, cancel := context.WithCancel(context.Background())
	fixture.workflow.Helper = cancelAfterHelperOperationV2{base: fixture.workflow.Helper, mutate: protectionhelper.OperationNginxReload, cancel: cancel}
	result, err := fixture.workflow.ApplyV2(ctx, ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err == nil || err.Error() != "ambiguous_result" || result.State != protectionoperations.StateReconcileRequired ||
		result.LeaseState != hostresources.EndpointLeaseMutationPending || fixture.nginx.Reloads != 1 || countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxRestore) != 0 {
		t.Fatalf("result=%#v calls=%v reloads=%d err=%v", result, fixture.nginx.Calls, fixture.nginx.Reloads, err)
	}
	fixture.workflow.Helper = nil
	manager, _ := restartWorkflowV2(t, fixture, 92)
	beforeReload, beforeSwitch := fixture.nginx.Reloads, countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch)
	results, err := manager.Recover(context.Background())
	if err != nil || len(results) != 1 || results[0].ToState != protectionoperations.StateApplied || fixture.nginx.Reloads != beforeReload ||
		countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != beforeSwitch {
		t.Fatalf("results=%#v calls=%v reloads=%d err=%v", results, fixture.nginx.Calls, fixture.nginx.Reloads, err)
	}
}

func TestWorkflowV2LeaseActivationAmbiguityRetainsCandidateLeaseAndCheckpoint(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	prepared := fixture.prepare(t, "v2-activation-ambiguous")
	fixture.provider.fail["activate"] = true
	result, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err == nil || err.Error() != "ambiguous_result" || result.State != protectionoperations.StateReconcileRequired ||
		result.LeaseState != hostresources.EndpointLeaseMutationPending || countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxRestore) != 0 {
		t.Fatalf("result=%#v calls=%v err=%v", result, fixture.nginx.Calls, err)
	}
	if _, loadErr := fixture.workflow.loadV2(prepared.OperationID); loadErr != nil {
		t.Fatalf("checkpoint was not retained: %v", loadErr)
	}
}

func TestWorkflowV2ManualRollbackRestoresExactPreviousAndReleasesLease(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	prepared := fixture.prepare(t, "v2-manual-rollback")
	applied, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fixture.workflow.RollbackV2(context.Background(), RollbackV2Input{OperationID: applied.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "ROLLBACK FRONTING " + applied.OperationID})
	if err != nil || result.State != protectionoperations.StateRolledBack || result.LeaseState != hostresources.EndpointLeaseReleased ||
		fixture.nginx.ActiveRevision != result.PreviousRevision || countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxRestore) != 1 {
		t.Fatalf("result=%#v active=%s calls=%v err=%v", result, fixture.nginx.ActiveRevision, fixture.nginx.Calls, err)
	}
}

func TestWorkflowV2RollbackDoesNotOverwriteListenerDrift(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	prepared := fixture.prepare(t, "v2-rollback-listener-drift")
	fixture.workflow.V2Health = func(_ context.Context, request FixedL4HealthRequestV2) (FixedL4HealthEvidenceV2, error) {
		fixture.nginx.RevisionListeners[request.CandidateRevision] = []protectionhelper.NginxListener{{Address: "127.0.0.1", Port: 6553}}
		return FixedL4HealthEvidenceV2{}, errors.New("health failed after listener drift")
	}
	result, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err == nil || err.Error() != "rollback_drift" || result.State != protectionoperations.StateReconcileRequired ||
		result.LeaseState != hostresources.EndpointLeaseMutationPending || countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxRestore) != 0 || fixture.nginx.Reloads != 1 {
		t.Fatalf("result=%#v calls=%v reloads=%d err=%v", result, fixture.nginx.Calls, fixture.nginx.Reloads, err)
	}
}

func TestWorkflowV2ExactVerificationClassifiesActiveProcessAndListenerMismatch(t *testing.T) {
	tests := []struct {
		name, code string
		mutate     func(*protectionhelper.NginxResult)
	}{
		{name: "active", code: "active_revision_mismatch", mutate: func(result *protectionhelper.NginxResult) { result.SHA256 = strings.Repeat("9", 64) }},
		{name: "process", code: "process_identity_mismatch", mutate: func(result *protectionhelper.NginxResult) { result.WorkerPIDs = nil }},
		{name: "listener", code: "listener_identity_mismatch", mutate: func(result *protectionhelper.NginxResult) { result.ListenersMatched = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
			prepared := fixture.prepare(t, "v2-verify-"+test.name)
			operation, err := fixture.workflow.operation(context.Background(), prepared.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			binary := fixture.nginx.Support.Binary
			fixture.workflow.Helper = helperFuncV2(func(_ context.Context, request protectionhelper.Request) (protectionhelper.Response, error) {
				result := &protectionhelper.NginxResult{Revision: request.NginxVerify.ExpectedRevision, SHA256: request.NginxVerify.ExpectedSHA256,
					Binary: binary, MasterPID: 100, WorkerPIDs: []int{201}, ListenersMatched: true}
				test.mutate(result)
				return protectionhelper.Response{OK: true, Nginx: result}, nil
			})
			_, code := fixture.workflow.verifyEngineRevisionV2(context.Background(), operation, strings.Repeat("a", 64), strings.Repeat("b", 64), binary,
				helperListenerV2(fixture.plan.PublicSocket))
			if code != test.code {
				t.Fatalf("code=%s want=%s", code, test.code)
			}
		})
	}
}

func TestWorkflowV2RollbackFailureRetainsLeaseAndBundle(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	prepared := fixture.prepare(t, "v2-rollback-failed")
	fixture.workflow.V2Health = func(context.Context, FixedL4HealthRequestV2) (FixedL4HealthEvidenceV2, error) {
		return FixedL4HealthEvidenceV2{}, errors.New("health failed")
	}
	fixture.nginx.Fail[protectionhelper.OperationNginxRestore] = errors.New("restore failed")
	result, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err == nil || result.State != protectionoperations.StateRollbackFailed || result.LeaseState == hostresources.EndpointLeaseReleased || fixture.bundles.count != 1 {
		t.Fatalf("result=%#v bundles=%d err=%v", result, fixture.bundles.count, err)
	}
}

func TestWorkflowV2RecoveryBundleIsBoundedAndRedacted(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	fixture.workflow.Recovery = protectionartifacts.OperationRecovery{Storage: fixture.storage, Repository: fixture.repository}
	prepared := fixture.prepare(t, "v2-recovery-bundle")
	fixture.workflow.V2Health = func(context.Context, FixedL4HealthRequestV2) (FixedL4HealthEvidenceV2, error) {
		return FixedL4HealthEvidenceV2{}, errors.New("health contained /private/path and secret")
	}
	fixture.nginx.Fail[protectionhelper.OperationNginxRestore] = errors.New("helper contained --argv secret")
	result, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err == nil || result.State != protectionoperations.StateRollbackFailed {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	data, readErr := os.ReadFile(filepath.Join(fixture.storage.Root(), "recovery", prepared.OperationID, "summary.json"))
	if readErr != nil || len(data) == 0 || len(data) > 32<<10 {
		t.Fatalf("bundle bytes=%d err=%v", len(data), readErr)
	}
	lower := strings.ToLower(string(data))
	for _, forbidden := range []string{"proxy_pass", "127.0.0.1", "/private/path", "--argv", "secret", "loader.conf", "usr/sbin"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("recovery summary leaked %q: %s", forbidden, data)
		}
	}
	for _, required := range []string{"l4_one_to_one_fronting", "planDigest", "providerLeaseRef", "rollbackAttemptCount"} {
		if !strings.Contains(string(data), required) {
			t.Fatalf("recovery summary missed %q: %s", required, data)
		}
	}
}

func TestWorkflowV2PlanAndProviderDriftFailClosedBeforeMutation(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	prepared := fixture.prepare(t, "v2-stale")
	fixture.source.mu.Lock()
	fixture.source.input.Socket.ListenerSocketFactRevision = v2Revision("changed-listener")
	fixture.source.input.Socket, _ = FinalizeFrontingSocketClaimV1(fixture.source.input.Socket)
	fixture.source.mu.Unlock()
	result, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err == nil || result.State != protectionoperations.StateCancelled || countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != 0 || fixture.nginx.Reloads != 0 || result.LeaseState != hostresources.EndpointLeaseReleased {
		t.Fatalf("result=%#v calls=%v reloads=%d err=%v", result, fixture.nginx.Calls, fixture.nginx.Reloads, err)
	}
}

func TestWorkflowV2ExpiryManagementAndProxyDriftUseTypedPreMutationFailures(t *testing.T) {
	tests := []struct {
		name  string
		mode  hostresources.ProxyMode
		code  string
		drift func(*workflowV2Fixture)
	}{
		{name: "expired", mode: hostresources.ProxyModeOff, code: "plan_expired", drift: func(f *workflowV2Fixture) {
			f.now = time.Unix(f.plan.ExpiresAt, 0).UTC()
		}},
		{name: "management", mode: hostresources.ProxyModeOff, code: "backend_management_forbidden", drift: func(f *workflowV2Fixture) {
			f.source.input.Inventory[0].CanReachManagement = hostresources.CapabilityYes
		}},
		{name: "proxy", mode: hostresources.ProxyModeOn, code: "proxy_protocol_mismatch", drift: func(f *workflowV2Fixture) {
			f.source.input.Inventory[0].AcceptsProxyProtocol = hostresources.CapabilityNo
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWorkflowV2Fixture(t, test.mode)
			prepared := fixture.prepare(t, "v2-drift-"+test.name)
			test.drift(fixture)
			result, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
				Confirmation: "APPLY FRONTING " + prepared.OperationID})
			if err == nil || err.Error() != test.code || result.State != protectionoperations.StateCancelled || result.LeaseState != hostresources.EndpointLeaseReleased ||
				countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != 0 || fixture.nginx.Reloads != 0 {
				t.Fatalf("result=%#v calls=%v err=%v", result, fixture.nginx.Calls, err)
			}
		})
	}
}

func TestWorkflowV2RestartReverifiesAppliedWithoutDuplicateMutation(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	prepared := fixture.prepare(t, "v2-restart-applied")
	applied, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err != nil || applied.State != protectionoperations.StateApplied {
		t.Fatalf("applied=%#v err=%v", applied, err)
	}
	manager, _ := restartWorkflowV2(t, fixture, 88)
	beforeSwitch := countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch)
	beforeReload := fixture.nginx.Reloads
	results, err := manager.Recover(context.Background())
	if err != nil || len(results) != 1 || results[0].ToState != protectionoperations.StateApplied ||
		countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != beforeSwitch || fixture.nginx.Reloads != beforeReload {
		t.Fatalf("results=%#v calls=%v reloads=%d err=%v", results, fixture.nginx.Calls, fixture.nginx.Reloads, err)
	}
	fixture.provider.mu.Lock()
	mutations := filterLeaseMutationsV2(fixture.provider.calls)
	fixture.provider.mu.Unlock()
	if strings.Join(mutations, ",") != "acquire,fence,activate" {
		t.Fatalf("restart repeated provider mutation: %v", mutations)
	}
}

func TestWorkflowV2RestartReleasesAmbiguousCommittedAcquire(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	fixture.provider.acquireCommittedThenError = true
	result, err := fixture.workflow.PrepareV2(context.Background(), PrepareV2Input{Plan: fixture.plan, Actor: "tester", IdempotencyKey: "v2-ambiguous-acquire",
		Confirmation: "PREPARE FRONTING " + fixture.plan.CanonicalPlanDigest})
	if err == nil || result.State != protectionoperations.StateReconcileRequired || result.LeaseID != "" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	manager, _ := restartWorkflowV2(t, fixture, 89)
	results, err := manager.Recover(context.Background())
	if err != nil || len(results) != 1 || results[0].ToState != protectionoperations.StateCancelled {
		t.Fatalf("results=%#v err=%v", results, err)
	}
	fixture.provider.mu.Lock()
	state := fixture.provider.lease.State
	mutations := filterLeaseMutationsV2(fixture.provider.calls)
	fixture.provider.mu.Unlock()
	if state != hostresources.EndpointLeaseReleased || strings.Join(mutations, ",") != "acquire,release" {
		t.Fatalf("lease state=%s mutations=%v", state, mutations)
	}
}

func TestWorkflowV2RestartNeverReappliesInterruptedRollback(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	prepared := fixture.prepare(t, "v2-restart-rollback-intent")
	applied, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err != nil {
		t.Fatal(err)
	}
	rolling, err := fixture.manager.BeginRollback(context.Background(), applied.OperationID, applied.OperationRevision)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := fixture.workflow.loadV2(applied.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint.OperationRevision, checkpoint.RollbackAttemptCount, checkpoint.FailedStage = rolling.Revision, 1, "health_failed"
	if err := fixture.workflow.saveV2(&checkpoint, "rollback_intent"); err != nil {
		t.Fatal(err)
	}
	manager, _ := restartWorkflowV2(t, fixture, 90)
	beforeReload, beforeSwitch := fixture.nginx.Reloads, countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch)
	results, err := manager.Recover(context.Background())
	if err != nil || len(results) != 1 || results[0].ToState != protectionoperations.StateRollbackFailed ||
		fixture.nginx.Reloads != beforeReload || countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != beforeSwitch {
		t.Fatalf("results=%#v reloads=%d calls=%v err=%v", results, fixture.nginx.Reloads, fixture.nginx.Calls, err)
	}
}

func TestWorkflowV2RestartNeverReappliesRollbackBeforeCheckpointWrite(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	prepared := fixture.prepare(t, "v2-restart-rollback-before-checkpoint")
	applied, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.BeginRollback(context.Background(), applied.OperationID, applied.OperationRevision); err != nil {
		t.Fatal(err)
	}
	manager, _ := restartWorkflowV2(t, fixture, 93)
	beforeReload, beforeSwitch := fixture.nginx.Reloads, countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch)
	fixture.provider.mu.Lock()
	beforeLeaseMutations := append([]string(nil), filterLeaseMutationsV2(fixture.provider.calls)...)
	fixture.provider.mu.Unlock()
	results, err := manager.Recover(context.Background())
	fixture.provider.mu.Lock()
	afterLeaseMutations := append([]string(nil), filterLeaseMutationsV2(fixture.provider.calls)...)
	fixture.provider.mu.Unlock()
	if err != nil || len(results) != 1 || results[0].ToState != protectionoperations.StateRollbackFailed || fixture.nginx.Reloads != beforeReload ||
		countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != beforeSwitch || strings.Join(afterLeaseMutations, ",") != strings.Join(beforeLeaseMutations, ",") {
		t.Fatalf("results=%#v calls=%v reloads=%d leaseBefore=%v leaseAfter=%v err=%v", results, fixture.nginx.Calls, fixture.nginx.Reloads, beforeLeaseMutations, afterLeaseMutations, err)
	}
}

func TestValidateHealthEvidenceV2RejectsFutureObservation(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	request := FixedL4HealthRequestV2{OperationID: "operation-health", OperationRevision: 7, PlanDigest: strings.Repeat("1", 64),
		CandidateRevision: strings.Repeat("2", 64), CandidateSHA256: strings.Repeat("3", 64), SocketClaimRevision: strings.Repeat("4", 64),
		BackendReferenceRevision: strings.Repeat("5", 64), LeaseRevision: strings.Repeat("6", 64), ProxyMode: hostresources.ProxyModeOff}
	evidence, err := fixture.workflow.V2Health(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	evidence.ObservedAt = fixture.now.Add(time.Minute).Unix()
	evidence.ExpiresAt = fixture.now.Add(time.Minute + 20*time.Second).Unix()
	if err := validateHealthEvidenceV2(request, evidence, fixture.now); err == nil {
		t.Fatal("future-dated health evidence was accepted")
	}
}

func TestWorkflowV2RestartDoesNotReleaseLeaseTwice(t *testing.T) {
	fixture := newWorkflowV2Fixture(t, hostresources.ProxyModeOff)
	prepared := fixture.prepare(t, "v2-restart-after-release")
	applied, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err != nil {
		t.Fatal(err)
	}
	rolling, err := fixture.manager.BeginRollback(context.Background(), applied.OperationID, applied.OperationRevision)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := fixture.workflow.loadV2(applied.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint.OperationRevision, checkpoint.RollbackAttemptCount = rolling.Revision, 1
	fixture.nginx.ActiveRevision, fixture.nginx.ActiveSHA256 = checkpoint.PreviousRevision, checkpoint.PreviousSHA256
	checkpoint.Detached, checkpoint.ActualActiveRevision = true, checkpoint.PreviousRevision
	fixture.provider.mu.Lock()
	lease := fixture.provider.lease
	fixture.provider.mu.Unlock()
	released, code := releaseLeaseV2(context.Background(), fixture.provider, lease, rolling.OperationID+"-release", detachmentRevisionV2(checkpoint), fixture.now)
	if code != "" {
		t.Fatal(code)
	}
	checkpoint.Lease = released
	if err := fixture.workflow.saveV2(&checkpoint, "lease_released"); err != nil {
		t.Fatal(err)
	}
	manager, _ := restartWorkflowV2(t, fixture, 91)
	results, err := manager.Recover(context.Background())
	if err != nil || len(results) != 1 || results[0].ToState != protectionoperations.StateRolledBack {
		t.Fatalf("results=%#v err=%v", results, err)
	}
	fixture.provider.mu.Lock()
	mutations := filterLeaseMutationsV2(fixture.provider.calls)
	fixture.provider.mu.Unlock()
	if strings.Join(mutations, ",") != "acquire,fence,activate,release" {
		t.Fatalf("restart repeated lease release: %v", mutations)
	}
}

func filterLeaseMutationsV2(values []string) []string {
	result := []string{}
	for _, value := range values {
		if value == "acquire" || value == "fence" || value == "activate" || value == "release" {
			result = append(result, value)
		}
	}
	return result
}

func restartWorkflowV2(t *testing.T, fixture *workflowV2Fixture, pid int) (*protectionoperations.Manager, *Workflow) {
	t.Helper()
	if err := fixture.manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager := protectionoperations.NewManager(fixture.repository, protectionoperations.Options{InstanceID: "fronting-v2-restart-" + fmt.Sprint(pid), PID: pid,
		Now: func() time.Time { return fixture.now }, Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil }})
	root, err := protectionhelper.NewManagedRoot(fixture.storage.Root())
	if err != nil {
		t.Fatal(err)
	}
	client, err := protectionhelper.NewClient(root, manager, fixture.nginx, frontingAudit{})
	if err != nil {
		t.Fatal(err)
	}
	workflow := &Workflow{Manager: manager, Helper: client, Artifacts: protectionartifacts.Service{Storage: fixture.storage, Store: fixture.repository},
		Marker: fixture.storage, State: fixture.storage, Recovery: fixture.bundles, Health: passingFrontingHealth, RollbackHealth: fixture.workflow.RollbackHealth,
		V2Plans: fixture.source, V2Leases: memoryLeaseDirectoryV2{provider: fixture.provider}, V2Artifacts: fixture.storage,
		V2Health: fixture.workflow.V2Health, Now: func() time.Time { return fixture.now }}
	if err := manager.SetReconcilerForKind(protectionoperations.KindFronting, V2Reconciler{Workflow: workflow}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	return manager, workflow
}

func filterNginxMutationsV2(values []protectionhelper.Operation) []string {
	result := []string{}
	for _, value := range values {
		switch value {
		case protectionhelper.OperationNginxValidate, protectionhelper.OperationNginxInstall, protectionhelper.OperationNginxSwitch,
			protectionhelper.OperationNginxReload, protectionhelper.OperationNginxVerify, protectionhelper.OperationNginxRestore:
			result = append(result, string(value))
		}
	}
	return result
}

func backendFactAddressV2(t *testing.T, now time.Time, address string, family hostresources.AddressFamily) hostresources.FrontingBackendFactV1 {
	t.Helper()
	endpoint := hostresources.PublicEndpoint{Schema: hostresources.EndpointSchemaV1, ID: "endpoint-ipv6",
		Key:    hostresources.PublicEndpointKey{Network: hostresources.NetworkTCP, AddressFamily: family, BindAddress: address, Port: 10001},
		Intent: hostresources.EndpointIntentLocal, Protocol: "tcp", ProxyProtocol: hostresources.CapabilityNo,
		ResourceID: "resource-ipv6", Owner: "core", OwnerRevision: "owner-v1", ConfigurationRevision: v2Revision("ipv6-config"),
		ObservedAt: now.Unix(), Source: "fixture", ConfidenceBP: 10_000}
	resource := hostresources.ProtectableResource{ID: endpoint.ResourceID, Kind: string(hostresources.FrontingBackendInboundResource), Owner: endpoint.Owner,
		Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, OwnerRevision: endpoint.OwnerRevision}, Endpoints: []hostresources.PublicEndpoint{endpoint}}
	fact, err := hostresources.NewFrontingBackendFactV1(hostresources.FrontingBackendFactV1{ProviderID: "provider", ContributorID: "contributor",
		ProviderRevision: "provider-v1", HealthRevision: v2Revision("ipv6-health"), CapacityRevision: v2Revision("ipv6-capacity"),
		Ownership: hostresources.FrontingBackendProviderManaged, CanReachManagement: hostresources.CapabilityNo, HealthReady: true, CapacityReady: true,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()}, resource, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	return fact
}

func rehashPlanV2(plan FrontingStrategyPlanV2) FrontingStrategyPlanV2 {
	plan.CanonicalPlanDigest = v2Revision(frontingPlanDigestInput(plan))
	plan.PlanID = "fronting_" + plan.CanonicalPlanDigest[:24]
	return plan
}
