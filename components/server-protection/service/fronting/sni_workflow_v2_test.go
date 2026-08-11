package fronting

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
)

type mapLeaseDirectoryV2 struct {
	providers map[string]*memoryEndpointLeaseProviderV2
}

func (d mapLeaseDirectoryV2) EndpointLeaseProviderV1(id string) (hostresources.EndpointLeaseProviderV1, bool) {
	provider, ok := d.providers[id]
	return provider, ok
}

func (d mapLeaseDirectoryV2) EndpointLeasesByHolderV1(ctx context.Context, holder string) ([]hostresources.EndpointLeaseV1, error) {
	ids := make([]string, 0, len(d.providers))
	for id := range d.providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := []hostresources.EndpointLeaseV1{}
	for _, id := range ids {
		leases, err := d.providers[id].ListEndpointLeases(ctx, hostresources.ListEndpointLeasesRequestV1{HolderID: holder, Limit: MaxFixedTargetsV1})
		if err != nil {
			return nil, err
		}
		for _, lease := range leases {
			if lease.HolderID == holder {
				result = append(result, lease)
			}
		}
	}
	return result, nil
}

type sniWorkflowFixtureV2 struct {
	frontingFixture
	now       time.Time
	plan      FrontingStrategyPlanV2
	source    *fixedPlanSourceV2
	providers map[string]*memoryEndpointLeaseProviderV2
	healthMu  sync.Mutex
	healths   int
}

func newSNIWorkflowFixtureV2(t *testing.T) *sniWorkflowFixtureV2 {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	input := sniPlanInputV2(t, now, SelectorDefaultV1{})
	for index := range input.Inventory {
		input.Inventory[index].ProviderID = "provider-" + decimalV2(index)
		input.Inventory[index].ProviderRevision = "provider-revision-" + decimalV2(index)
		input.BackendReferences[index], _ = hostresources.ReferenceFrontingBackendV1(input.Inventory[index], hostresources.ProxyModeOff, now)
	}
	input.Selectors, _ = CanonicalizeSelectorSetV1([]SelectorRouteInputV1{
		{SNI: "route.example", TargetReferenceRevision: input.BackendReferences[0].CanonicalReferenceRevision},
		{SNI: "alternate.example", TargetReferenceRevision: input.BackendReferences[1].CanonicalReferenceRevision},
	}, SelectorDefaultV1{})
	plan, err := PlanFrontingStrategyV2(input)
	if err != nil || len(plan.Safety.Blocks) != 0 || plan.Strategy.Selected != StrategySNIPreread {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	base := newFrontingFixture(t, passingFrontingHealth)
	providers := map[string]*memoryEndpointLeaseProviderV2{}
	for _, reference := range plan.Targets.BackendReferences {
		providers[reference.ProviderID] = &memoryEndpointLeaseProviderV2{id: reference.ProviderID, now: now, fail: map[string]bool{}}
	}
	source := &fixedPlanSourceV2{input: input}
	fixture := &sniWorkflowFixtureV2{frontingFixture: base, now: now, plan: plan, source: source, providers: providers}
	base.workflow.Now = func() time.Time { return fixture.now }
	base.workflow.V2Plans = source
	base.workflow.V2Leases = mapLeaseDirectoryV2{providers: providers}
	base.workflow.V2Artifacts = base.storage
	base.workflow.V2Health = func(context.Context, FixedL4HealthRequestV2) (FixedL4HealthEvidenceV2, error) {
		return FixedL4HealthEvidenceV2{}, errors.New("wrong strategy health")
	}
	base.workflow.V2SNIHealth = func(_ context.Context, request SNIPrereadHealthRequestV2) (SNIPrereadHealthEvidenceV2, error) {
		fixture.healthMu.Lock()
		fixture.healths++
		fixture.healthMu.Unlock()
		probes := make([]SNIHealthProbeEvidenceV2, 0, len(request.Probes))
		for _, probe := range request.Probes {
			evidence := SNIHealthProbeEvidenceV2{ProbeID: probe.ProbeID, ExpectedTargetRevision: probe.ExpectedTargetRevision}
			if probe.ExpectReject {
				evidence.ConnectionRejected = true
			} else {
				evidence.ExpectedBackendReached = true
				evidence.ObservedTargetRevision = probe.ExpectedTargetRevision
				evidence.BackendIdentityMarker = probe.ExpectedTargetRevision
				evidence.ProxyHeaderObserved = request.ProxyMode == hostresources.ProxyModeOn
			}
			probes = append(probes, evidence)
		}
		return SNIPrereadHealthEvidenceV2{Schema: SNIPrereadHealthSchemaV2, OperationID: request.OperationID, OperationRevision: request.OperationRevision,
			PlanDigest: request.PlanDigest, CandidateRevision: request.CandidateRevision, CandidateSHA256: request.CandidateSHA256,
			SocketClaimRevision: request.SocketClaimRevision, SelectorSetRevision: request.SelectorSetRevision, MapRevision: request.MapRevision,
			UpstreamIDSetRevision: request.UpstreamIDSetRevision, TargetAuthorityRevisions: append([]string(nil), request.TargetAuthorityRevisions...),
			ProxyMode: request.ProxyMode, Probes: probes, ObservedAt: fixture.now.Unix(), ExpiresAt: fixture.now.Add(20 * time.Second).Unix()}, nil
	}
	return fixture
}

func (f *sniWorkflowFixtureV2) prepare(t *testing.T, key string) WorkflowResultV2 {
	t.Helper()
	result, err := f.workflow.PrepareV2(context.Background(), PrepareV2Input{Plan: f.plan, Actor: "tester", IdempotencyKey: key,
		Confirmation: "PREPARE FRONTING " + f.plan.CanonicalPlanDigest})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSNIWorkflowV2UsesExistingEngineAndAllExactLeases(t *testing.T) {
	fixture := newSNIWorkflowFixtureV2(t)
	prepared := fixture.prepare(t, "sni-v2-happy")
	if prepared.State != protectionoperations.StatePrepared || len(prepared.TargetAuthorityRevisions) != 2 || prepared.LeaseID != "" {
		t.Fatalf("prepared=%#v", prepared)
	}
	checkpoint, err := fixture.workflow.loadV2(prepared.OperationID)
	if err != nil || len(checkpoint.EndpointLeases) != 2 || checkpoint.MapRevision == "" || checkpoint.UpstreamIDSetRevision == "" ||
		checkpoint.SelectorSetRevision != fixture.plan.Selectors.SelectorSetRevision || !fixture.storage.HasMutationMarker(prepared.OperationID) && checkpoint.MutationMarker {
		t.Fatalf("checkpoint=%#v err=%v", checkpoint, err)
	}
	for _, provider := range fixture.providers {
		provider.mu.Lock()
		state, calls := provider.lease.State, append([]string(nil), provider.calls...)
		provider.mu.Unlock()
		if state != hostresources.EndpointLeaseReserved || strings.Join(filterLeaseMutationsV2(calls), ",") != "acquire" {
			t.Fatalf("provider state=%s calls=%v", state, calls)
		}
	}
	applied, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err != nil || applied.State != protectionoperations.StateApplied || len(applied.TargetAuthorityRevisions) != 2 ||
		countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != 1 || fixture.nginx.Reloads != 1 {
		t.Fatalf("applied=%#v calls=%v reloads=%d err=%v", applied, fixture.nginx.Calls, fixture.nginx.Reloads, err)
	}
	for _, provider := range fixture.providers {
		provider.mu.Lock()
		state, mutations := provider.lease.State, filterLeaseMutationsV2(provider.calls)
		provider.mu.Unlock()
		if state != hostresources.EndpointLeaseActive || strings.Join(mutations, ",") != "acquire,fence,activate" {
			t.Fatalf("provider state=%s mutations=%v", state, mutations)
		}
	}
	replayed, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err != nil || replayed.State != protectionoperations.StateApplied || countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != 1 || fixture.nginx.Reloads != 1 {
		t.Fatalf("historical replay=%#v calls=%v reloads=%d err=%v", replayed, fixture.nginx.Calls, fixture.nginx.Reloads, err)
	}
	rolled, err := fixture.workflow.RollbackV2(context.Background(), RollbackV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "ROLLBACK FRONTING " + prepared.OperationID})
	if err != nil || rolled.State != protectionoperations.StateRolledBack {
		t.Fatalf("rolled=%#v err=%v", rolled, err)
	}
	for _, provider := range fixture.providers {
		provider.mu.Lock()
		state, mutations := provider.lease.State, filterLeaseMutationsV2(provider.calls)
		provider.mu.Unlock()
		if state != hostresources.EndpointLeaseReleased || strings.Join(mutations, ",") != "acquire,fence,activate,release" {
			t.Fatalf("provider state=%s mutations=%v", state, mutations)
		}
	}
}

func TestSNIWorkflowV2PartialAcquireCleansOnlyAcquiredAuthority(t *testing.T) {
	fixture := newSNIWorkflowFixtureV2(t)
	specs, err := targetAuthoritySpecsV2(fixture.plan)
	if err != nil {
		t.Fatal(err)
	}
	secondProvider := specs[1].Endpoint.ProviderID
	fixture.providers[secondProvider].fail["acquire"] = true
	result, err := fixture.workflow.PrepareV2(context.Background(), PrepareV2Input{Plan: fixture.plan, Actor: "tester", IdempotencyKey: "sni-v2-partial",
		Confirmation: "PREPARE FRONTING " + fixture.plan.CanonicalPlanDigest})
	if err == nil || result.State != protectionoperations.StateCancelled || fixture.storage.HasMutationMarker(result.OperationID) ||
		countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != 0 || fixture.nginx.Reloads != 0 {
		t.Fatalf("result=%#v calls=%v err=%v", result, fixture.nginx.Calls, err)
	}
	first := fixture.providers[specs[0].Endpoint.ProviderID]
	first.mu.Lock()
	firstState, firstMutations := first.lease.State, filterLeaseMutationsV2(first.calls)
	first.mu.Unlock()
	second := fixture.providers[secondProvider]
	second.mu.Lock()
	secondMutations := filterLeaseMutationsV2(second.calls)
	second.mu.Unlock()
	if firstState != hostresources.EndpointLeaseReleased || strings.Join(firstMutations, ",") != "acquire,release" || strings.Join(secondMutations, ",") != "acquire" {
		t.Fatalf("first=%s/%v second=%v", firstState, firstMutations, secondMutations)
	}
}

func TestSNIWorkflowV2RestartReleasesAllAuthoritiesAfterAmbiguousLaterAcquire(t *testing.T) {
	fixture := newSNIWorkflowFixtureV2(t)
	specs, err := targetAuthoritySpecsV2(fixture.plan)
	if err != nil {
		t.Fatal(err)
	}
	fixture.providers[specs[1].Endpoint.ProviderID].acquireCommittedThenError = true
	result, err := fixture.workflow.PrepareV2(context.Background(), PrepareV2Input{Plan: fixture.plan, Actor: "tester", IdempotencyKey: "sni-v2-ambiguous-later-acquire",
		Confirmation: "PREPARE FRONTING " + fixture.plan.CanonicalPlanDigest})
	if err == nil || result.State != protectionoperations.StateReconcileRequired {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	checkpoint, loadErr := fixture.workflow.loadV2(result.OperationID)
	if loadErr != nil || len(checkpoint.EndpointLeases) != 1 {
		t.Fatalf("incomplete checkpoint=%#v err=%v", checkpoint, loadErr)
	}
	manager := restartSNIWorkflowV2(t, fixture, 110)
	results, recoverErr := manager.Recover(context.Background())
	if recoverErr != nil || len(results) != 1 || results[0].ToState != protectionoperations.StateCancelled {
		t.Fatalf("results=%#v err=%v", results, recoverErr)
	}
	for _, provider := range fixture.providers {
		provider.mu.Lock()
		state := provider.lease.State
		mutations := filterLeaseMutationsV2(provider.calls)
		provider.mu.Unlock()
		if state != hostresources.EndpointLeaseReleased || strings.Join(mutations, ",") != "acquire,release" {
			t.Fatalf("ambiguous acquired authority was not safely released: state=%s mutations=%v", state, mutations)
		}
	}
}

func TestSNIWorkflowV2PersistsPartialFenceAndActivationBeforeRollback(t *testing.T) {
	for _, stage := range []string{"fence", "activate"} {
		t.Run(stage, func(t *testing.T) {
			fixture := newSNIWorkflowFixtureV2(t)
			specs, err := targetAuthoritySpecsV2(fixture.plan)
			if err != nil {
				t.Fatal(err)
			}
			fixture.providers[specs[1].Endpoint.ProviderID].fail[stage] = true
			prepared := fixture.prepare(t, "sni-v2-partial-"+stage)
			result, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
				Confirmation: "APPLY FRONTING " + prepared.OperationID})
			if err == nil || result.State != protectionoperations.StateReconcileRequired {
				t.Fatalf("stage=%s result=%#v active=%s err=%v", stage, result, fixture.nginx.ActiveRevision, err)
			}
			checkpoint, loadErr := fixture.workflow.loadV2(prepared.OperationID)
			if loadErr != nil || len(checkpoint.EndpointLeases) != 2 || checkpoint.Detached {
				t.Fatalf("stage=%s checkpoint=%#v err=%v", stage, checkpoint, loadErr)
			}
			states := map[string]hostresources.EndpointLeaseState{}
			for _, lease := range checkpoint.EndpointLeases {
				states[lease.ExactReference.ProviderID] = lease.State
			}
			wantFirst := hostresources.EndpointLeaseMutationPending
			if stage == "activate" {
				wantFirst = hostresources.EndpointLeaseActive
			}
			wantSecond := hostresources.EndpointLeaseReserved
			if stage == "activate" {
				wantSecond = hostresources.EndpointLeaseMutationPending
			}
			if states[specs[0].Endpoint.ProviderID] != wantFirst || states[specs[1].Endpoint.ProviderID] != wantSecond {
				t.Fatalf("stage=%s partial authority progress not persisted: %#v", stage, states)
			}
		})
	}
}

func TestSNIWorkflowV2PartialReleaseFailurePersistsReleasedAuthority(t *testing.T) {
	fixture := newSNIWorkflowFixtureV2(t)
	specs, err := targetAuthoritySpecsV2(fixture.plan)
	if err != nil {
		t.Fatal(err)
	}
	prepared := fixture.prepare(t, "sni-v2-partial-release")
	fixture.workflow.V2SNIHealth = func(context.Context, SNIPrereadHealthRequestV2) (SNIPrereadHealthEvidenceV2, error) {
		return SNIPrereadHealthEvidenceV2{}, errors.New("health failure")
	}
	fixture.providers[specs[1].Endpoint.ProviderID].fail["release"] = true
	result, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err == nil || result.State != protectionoperations.StateRollbackFailed {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	checkpoint, loadErr := fixture.workflow.loadV2(prepared.OperationID)
	if loadErr != nil || len(checkpoint.EndpointLeases) != 2 {
		t.Fatalf("checkpoint=%#v err=%v", checkpoint, loadErr)
	}
	states := map[string]hostresources.EndpointLeaseState{}
	for _, lease := range checkpoint.EndpointLeases {
		states[lease.ExactReference.ProviderID] = lease.State
	}
	if states[specs[0].Endpoint.ProviderID] != hostresources.EndpointLeaseReleased || states[specs[1].Endpoint.ProviderID] == hostresources.EndpointLeaseReleased {
		t.Fatalf("partial release progress was not persisted: %#v", states)
	}
}

func TestSNIWorkflowV2HealthFailureRestoresPreviousAndReleasesAfterDetach(t *testing.T) {
	fixture := newSNIWorkflowFixtureV2(t)
	prepared := fixture.prepare(t, "sni-v2-health-failure")
	fixture.workflow.V2SNIHealth = func(context.Context, SNIPrereadHealthRequestV2) (SNIPrereadHealthEvidenceV2, error) {
		return SNIPrereadHealthEvidenceV2{}, errors.New("typed health failure")
	}
	result, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err == nil || result.State != protectionoperations.StateRolledBack || fixture.nginx.ActiveRevision != result.PreviousRevision {
		t.Fatalf("result=%#v active=%s err=%v", result, fixture.nginx.ActiveRevision, err)
	}
	checkpoint, loadErr := fixture.workflow.loadV2(prepared.OperationID)
	if loadErr != nil || !checkpoint.Detached || checkpoint.ActualActiveRevision != checkpoint.PreviousRevision || checkpoint.RollbackAttemptCount != 1 {
		t.Fatalf("checkpoint=%#v err=%v", checkpoint, loadErr)
	}
	for _, provider := range fixture.providers {
		provider.mu.Lock()
		state := provider.lease.State
		provider.mu.Unlock()
		if state != hostresources.EndpointLeaseReleased {
			t.Fatalf("authority released before/without exact rollback detach: %s", state)
		}
	}
}

func TestSNIWorkflowV2RecoveryBundleBindsMapsAndAuthoritiesWithoutSelectors(t *testing.T) {
	fixture := newSNIWorkflowFixtureV2(t)
	fixture.workflow.Recovery = protectionartifacts.OperationRecovery{Storage: fixture.storage, Repository: fixture.repository}
	prepared := fixture.prepare(t, "sni-v2-recovery")
	fixture.workflow.V2SNIHealth = func(context.Context, SNIPrereadHealthRequestV2) (SNIPrereadHealthEvidenceV2, error) {
		return SNIPrereadHealthEvidenceV2{}, errors.New("route.example h2 /private/path secret")
	}
	fixture.nginx.Fail[protectionhelper.OperationNginxRestore] = errors.New("helper argv secret")
	result, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err == nil || result.State != protectionoperations.StateRollbackFailed {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	data, readErr := os.ReadFile(filepath.Join(fixture.storage.Root(), "recovery", prepared.OperationID, "summary.json"))
	if readErr != nil || len(data) == 0 || len(data) > 32<<10 {
		t.Fatalf("summary bytes=%d err=%v", len(data), readErr)
	}
	text := string(data)
	for _, required := range []string{"sni_preread_fronting", "selectorSetRevision", "mapRevision", "upstreamIdSetRevision", "targetReferenceRevisions", "targetAuthorities", "endpoint_lease"} {
		if !strings.Contains(text, required) {
			t.Fatalf("summary lacks %q: %s", required, text)
		}
	}
	for _, forbidden := range []string{"route.example", "h2", "/private/path", "secret", "127.0.0.1", "proxy_pass"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("summary leaked %q: %s", forbidden, text)
		}
	}
}

func TestSNIWorkflowV2RestartReverifiesWithoutDuplicateMutation(t *testing.T) {
	fixture := newSNIWorkflowFixtureV2(t)
	prepared := fixture.prepare(t, "sni-v2-restart")
	applied, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err != nil || applied.State != protectionoperations.StateApplied {
		t.Fatalf("applied=%#v err=%v", applied, err)
	}
	if err := fixture.manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager := protectionoperations.NewManager(fixture.repository, protectionoperations.Options{InstanceID: "sni-fronting-v2-restart", PID: 109,
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
		V2Plans: fixture.source, V2Leases: mapLeaseDirectoryV2{providers: fixture.providers}, V2Artifacts: fixture.storage,
		V2Health: fixture.workflow.V2Health, V2SNIHealth: fixture.workflow.V2SNIHealth, Now: func() time.Time { return fixture.now }}
	if err := manager.SetReconcilerForKind(protectionoperations.KindFronting, V2Reconciler{Workflow: workflow}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	beforeSwitch, beforeReload := countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch), fixture.nginx.Reloads
	results, err := manager.Recover(context.Background())
	if err != nil || len(results) != 1 || results[0].ToState != protectionoperations.StateApplied ||
		countOperation(fixture.nginx.Calls, protectionhelper.OperationNginxSwitch) != beforeSwitch || fixture.nginx.Reloads != beforeReload {
		t.Fatalf("results=%#v calls=%v reloads=%d err=%v pid=%s", results, fixture.nginx.Calls, fixture.nginx.Reloads, err, fmt.Sprint(109))
	}
	for _, provider := range fixture.providers {
		provider.mu.Lock()
		mutations := filterLeaseMutationsV2(provider.calls)
		provider.mu.Unlock()
		if strings.Join(mutations, ",") != "acquire,fence,activate" {
			t.Fatalf("restart repeated provider mutation: %v", mutations)
		}
	}
}

func TestSNIWorkflowV2HistoricalAppliedRequiresRecordedWorkerEvidence(t *testing.T) {
	fixture := newSNIWorkflowFixtureV2(t)
	prepared := fixture.prepare(t, "sni-v2-historical-worker-evidence")
	applied, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err != nil || applied.State != protectionoperations.StateApplied {
		t.Fatalf("applied=%#v err=%v", applied, err)
	}
	checkpoint, err := fixture.workflow.loadV2(prepared.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint.WorkerSetIdentityRevision = ""
	if err := fixture.workflow.saveV2(&checkpoint, "test_worker_evidence_missing"); err != nil {
		t.Fatal(err)
	}
	fixture.healthMu.Lock()
	healthBefore := fixture.healths
	fixture.healthMu.Unlock()
	replayed, err := fixture.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: fixture.plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err == nil || err.Error() != "active_revision_mismatch" || replayed.State != protectionoperations.StateApplied {
		t.Fatalf("historical replay=%#v err=%v", replayed, err)
	}
	fixture.healthMu.Lock()
	healthAfter := fixture.healths
	fixture.healthMu.Unlock()
	if healthAfter != healthBefore {
		t.Fatalf("health ran without worker evidence: %d -> %d", healthBefore, healthAfter)
	}
}

func restartSNIWorkflowV2(t *testing.T, fixture *sniWorkflowFixtureV2, pid int) *protectionoperations.Manager {
	t.Helper()
	if err := fixture.manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager := protectionoperations.NewManager(fixture.repository, protectionoperations.Options{InstanceID: "sni-fronting-v2-restart-" + fmt.Sprint(pid), PID: pid,
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
		V2Plans: fixture.source, V2Leases: mapLeaseDirectoryV2{providers: fixture.providers}, V2Artifacts: fixture.storage,
		V2Health: fixture.workflow.V2Health, V2SNIHealth: fixture.workflow.V2SNIHealth, Now: func() time.Time { return fixture.now }}
	if err := manager.SetReconcilerForKind(protectionoperations.KindFronting, V2Reconciler{Workflow: workflow}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	return manager
}
