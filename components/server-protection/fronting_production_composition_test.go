//go:build !minimal

package serverprotection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	helperinvoker "github.com/MalenkiySolovey/solovey-ui/components/server-protection/internal/normalci/helperinvoker"
	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	protectionfronting "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/fronting"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProductionComponentWiresCompleteV2AuthorityComposition(t *testing.T) {
	storage, err := protectionartifacts.New(filepath.Join(t.TempDir(), ".runtime", "server-protection"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := &protectionfronting.Workflow{}
	source := wireProductionFrontingV2(workflow, storage)
	if source == nil || workflow.V2Plans != source || workflow.V2Leases != hostresources.DefaultFrontingBackendsV1 ||
		workflow.V2Fallbacks != neutralfallback.Default || workflow.V2Artifacts != storage || workflow.V2Health == nil || workflow.V2SNIHealth == nil {
		t.Fatalf("incomplete production V2 composition: %#v", workflow)
	}
	for name, dependency := range map[string]any{"plans": workflow.V2Plans, "leases": workflow.V2Leases, "fallbacks": workflow.V2Fallbacks, "artifacts": workflow.V2Artifacts} {
		typeName := reflect.TypeOf(dependency).String()
		if strings.Contains(strings.ToLower(typeName), "fake") || strings.Contains(typeName, "_test") {
			t.Fatalf("production %s dependency is a test double: %s", name, typeName)
		}
	}
}

func TestProductionHealthCompositionFailsClosedWithoutTrafficOwner(t *testing.T) {
	registry := protectionfronting.NewExactHealthRegistryV2()
	if _, err := registry.FixedL4Check()(t.Context(), protectionfronting.FixedL4HealthRequestV2{}); err == nil {
		t.Fatal("fixed-L4 health succeeded without a traffic-evidence owner")
	}
	if _, err := registry.SNIPrereadCheck()(t.Context(), protectionfronting.SNIPrereadHealthRequestV2{}); err == nil {
		t.Fatal("SNI health succeeded without a traffic-evidence owner")
	}
}

func TestProductionSourceExactL4AndSNIOnlyPreviewUseRegistriesWithoutAuthorityMutation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	fixture := newProductionSourceFixture(t, now)
	service := &protectionfronting.SemanticServiceV2{Source: fixture.source, Now: func() time.Time { return now }}
	resources, err := fixture.source.ResourcesV2(t.Context(), now)
	if err != nil || len(resources) != 1 || len(resources[0].SocketClaims) != 1 || len(resources[0].BackendReferences) != 1 {
		t.Fatalf("production facts resources=%#v err=%v", resources, err)
	}
	resource := resources[0]
	base := protectionfronting.FrontingPreviewRequestV2{
		ResourceID: resource.ResourceID, ExpectedCurrentConfigurationRevision: resource.CurrentConfigurationRevision,
		RequestedStrategy: protectionfronting.StrategyL4OneToOne,
		SocketClaim: protectionfronting.FrontingSocketClaimReferenceV2{ResourceID: resource.ResourceID,
			EndpointID: resource.SocketClaims[0].EndpointID, ClaimRevision: resource.SocketClaims[0].ClaimRevision},
		BackendReferences: resource.BackendReferences, SelectedProxyMode: hostresources.ProxyModeOff,
	}
	l4, err := service.Preview(t.Context(), base)
	if err != nil || l4.Strategy.Selected != protectionfronting.StrategyL4OneToOne || l4.Strategy.Actual != protectionfronting.FrontingActualNotAppliedV2 {
		t.Fatalf("L4 preview=%#v err=%v", l4, err)
	}
	target := resource.BackendReferences[0].CanonicalReferenceRevision
	sniRequest := base
	sniRequest.RequestedStrategy = protectionfronting.StrategySNIPreread
	sniRequest.Selectors = []protectionfronting.SelectorRouteInputV1{{SNI: "route.example", TargetReferenceRevision: target}}
	sni, err := service.Preview(t.Context(), sniRequest)
	if err != nil || sni.Strategy.Selected != protectionfronting.StrategySNIPreread || sni.Strategy.Actual != protectionfronting.FrontingActualNotAppliedV2 {
		t.Fatalf("SNI preview=%#v err=%v", sni, err)
	}
	alpn := sniRequest
	alpn.Selectors[0].ALPN = []string{"h2"}
	if _, err := service.Preview(t.Context(), alpn); err == nil || err.Error() != "alpn_routing_unsupported" {
		t.Fatalf("ALPN production preview error=%v", err)
	}
	if fixture.provider.leaseCalls.Load() != 0 {
		t.Fatalf("preview made %d lease/mutation calls", fixture.provider.leaseCalls.Load())
	}
}

func TestProductionSourceMissingBackendProviderFailsClosed(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	fixture := newProductionSourceFixture(t, now)
	resources, err := fixture.source.ResourcesV2(t.Context(), now)
	if err != nil || len(resources) != 1 || len(resources[0].BackendReferences) != 1 {
		t.Fatalf("fixture facts=%#v err=%v", resources, err)
	}
	fixture.source.backends = hostresources.NewFrontingBackendRegistryV1()
	request := protectionfronting.FrontingPreviewRequestV2{
		ResourceID: resources[0].ResourceID, ExpectedCurrentConfigurationRevision: resources[0].CurrentConfigurationRevision,
		RequestedStrategy: protectionfronting.StrategyL4OneToOne,
		SocketClaim: protectionfronting.FrontingSocketClaimReferenceV2{ResourceID: resources[0].ResourceID,
			EndpointID: resources[0].SocketClaims[0].EndpointID, ClaimRevision: resources[0].SocketClaims[0].ClaimRevision},
		BackendReferences: resources[0].BackendReferences, SelectedProxyMode: hostresources.ProxyModeOff,
	}
	selectors, _ := protectionfronting.CanonicalizeSelectorSetV1(nil, protectionfronting.SelectorDefaultV1{})
	if _, err := fixture.source.ResolvePreviewV2(t.Context(), request, selectors, now); err == nil || err.Error() != "target_reference_stale" {
		t.Fatalf("missing provider error=%v", err)
	}
}

func TestProductionSourceUnchangedFactsPreserveCurrentPlanBindingsAcrossFreshnessRefresh(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	fixture, request, plan := productionL4PlanFixture(t, now)
	cached, ok := fixture.source.cached(plan.CanonicalPlanDigest)
	if !ok {
		t.Fatal("preview plan was not retained for prepare revalidation")
	}
	refreshedAt := now.Add(2 * time.Second)
	reread, err := fixture.source.resolveInput(t.Context(), request, cached.selectors, refreshedAt)
	if err != nil {
		t.Fatalf("unchanged production facts failed reread: %v", err)
	}
	current, err := fixture.source.currentFrontingPlanInputV2At(t.Context(), plan, refreshedAt)
	if err != nil || !reflect.DeepEqual(current, cached.input) {
		t.Fatalf("current plan input changed across freshness refresh: current=%#v cached=%#v err=%v", current, cached.input, err)
	}
	rereadPlan, err := protectionfronting.PlanFrontingStrategyV2(reread)
	if err != nil {
		t.Fatal(err)
	}
	backend := plan.Targets.BackendReferences[0]
	providerSetRevision := hostresources.Revision(struct{ ProviderID, ProviderRevision string }{backend.ProviderID, backend.ProviderRevision})
	defaultPolicyRevision := hostresources.Revision(plan.Selectors.Default)
	if reread.Runtime.CanonicalRuntimeIdentityRevision != cached.input.Runtime.CanonicalRuntimeIdentityRevision ||
		reread.Socket.ClaimRevision != cached.input.Socket.ClaimRevision ||
		reread.Socket.ListenerSocketFactRevision != cached.input.Socket.ListenerSocketFactRevision ||
		rereadPlan.StrategyCapabilityRevision != plan.StrategyCapabilityRevision ||
		!reflect.DeepEqual(reread.BackendReferences, cached.input.BackendReferences) ||
		reread.Selectors.SelectorSetRevision != cached.input.Selectors.SelectorSetRevision {
		t.Fatalf("unchanged canonical bindings changed: runtime=%s/%s capability=%s/%s socket=%s/%s listener=%s/%s backend=%#v/%#v selector=%s/%s",
			cached.input.Runtime.CanonicalRuntimeIdentityRevision, reread.Runtime.CanonicalRuntimeIdentityRevision,
			plan.StrategyCapabilityRevision, rereadPlan.StrategyCapabilityRevision, cached.input.Socket.ClaimRevision, reread.Socket.ClaimRevision,
			cached.input.Socket.ListenerSocketFactRevision, reread.Socket.ListenerSocketFactRevision, cached.input.BackendReferences,
			reread.BackendReferences, cached.input.Selectors.SelectorSetRevision, reread.Selectors.SelectorSetRevision)
	}
	t.Logf("plan_id=%s digest=%s created_at=%d expires_at=%d current_now=%d requested=%s selected=%s runtime_revision=%s capability_revision=%s socket_revision=%s configuration_revision=%s topology_revision=%s listener_revision=%s management_revision=%s backend_reference_revision=%s backend_endpoint_revision=%s backend_provider_revision=%s backend_health_revision=%s backend_capacity_revision=%s proxy_mode=%s selector_revision=%s default_policy_revision=%s provider_set_revision=%s",
		plan.PlanID, plan.CanonicalPlanDigest, plan.CreatedAt, plan.ExpiresAt, refreshedAt.Unix(), request.RequestedStrategy, plan.Strategy.Selected,
		plan.Runtime.IdentityRevision, plan.StrategyCapabilityRevision, plan.PublicSocket.ClaimRevision, plan.PublicSocket.CurrentConfigurationRevision,
		plan.PublicSocket.TopologyOwnershipEligibilityRevision, plan.PublicSocket.ListenerSocketFactRevision, plan.PublicSocket.ManagementExclusionRevision,
		backend.CanonicalReferenceRevision, backend.EndpointRevision, backend.ProviderRevision, backend.HealthRevision, backend.CapacityRevision,
		plan.Targets.SelectedProxyMode, plan.Selectors.SelectorSetRevision, defaultPolicyRevision, providerSetRevision)
	for _, expiry := range plan.Safety.InputExpiries {
		t.Logf("input_expiry kind=%s revision=%s expires_at=%d", expiry.Kind, expiry.Revision, expiry.ExpiresAt)
	}
}

func TestProductionSourceExpiredAndChangedBindingsReturnPlanStale(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	t.Run("expired", func(t *testing.T) {
		fixture, _, plan := productionL4PlanFixture(t, now)
		if _, err := fixture.source.currentFrontingPlanInputV2At(t.Context(), plan, time.Unix(plan.ExpiresAt, 0).UTC()); err == nil || err.Error() != "plan_stale" {
			t.Fatalf("expired plan input error=%v", err)
		}
	})
	t.Run("runtime revision", func(t *testing.T) {
		fixture, _, plan := productionL4PlanFixture(t, now)
		changedRuntime := productionReadyRuntimeFixture(t, now, plan.PublicSocket.ManagementExclusionRevision)
		fixture.source.runtime = func(context.Context, string, time.Time) (protectionfronting.NginxRuntimeIdentityV2, error) {
			return changedRuntime, nil
		}
		expectProductionPlanStale(t, fixture.source, plan, now)
	})
	t.Run("socket topology revision", func(t *testing.T) {
		fixture, _, plan := productionL4PlanFixture(t, now)
		resource, surface := productionPublicResourceFixture(now)
		resource.Capabilities.ExpectedListenerOwner.ContractRevision = strings.Repeat("d", 64)
		surface.ListenerOwner.Application.OwnerContractRevision = resource.Capabilities.ExpectedListenerOwner.ContractRevision
		surface.ListenerOwner.Seal()
		snapshot := hostfacts.Snapshot{GeneratedAt: now.Unix(), Facts: []hostfacts.HostSurfaceFactV1{surface}}
		snapshot.OwnerObservationRevision = hostfacts.OwnerObservationSetRevision(snapshot.Facts, []string{"fixture:" + surface.ListenerOwner.ObservationRevision})
		fixture.source.resources = func(context.Context) protectionresources.InventorySnapshot {
			return protectionresources.InventorySnapshot{GeneratedAt: now.Unix(), Resources: []hostresources.ProtectableResource{resource}}
		}
		fixture.source.surfaces = func() hostfacts.Snapshot { return snapshot }
		expectProductionPlanStale(t, fixture.source, plan, now)
	})
	for _, test := range []struct {
		name   string
		change func(*hostresources.FrontingBackendFactV1)
	}{
		{"backend endpoint revision", func(fact *hostresources.FrontingBackendFactV1) {
			fact.EndpointRevision = hostresources.Revision("changed-endpoint")
		}},
		{"backend provider revision", func(fact *hostresources.FrontingBackendFactV1) { fact.ProviderRevision = "core-fronting-v2" }},
		{"backend health revision", func(fact *hostresources.FrontingBackendFactV1) {
			fact.HealthRevision = hostresources.Revision("changed-health")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, _, plan := productionL4PlanFixture(t, now)
			test.change(&fixture.provider.fact)
			expectProductionPlanStale(t, fixture.source, plan, now)
		})
	}
}

func productionL4PlanFixture(t *testing.T, now time.Time) (productionSourceFixture, protectionfronting.FrontingPreviewRequestV2, protectionfronting.FrontingStrategyPlanV2) {
	t.Helper()
	fixture := newProductionSourceFixture(t, now)
	resources, err := fixture.source.ResourcesV2(t.Context(), now)
	if err != nil || len(resources) != 1 || len(resources[0].SocketClaims) != 1 || len(resources[0].BackendReferences) != 1 {
		t.Fatalf("production facts=%#v err=%v", resources, err)
	}
	request := protectionfronting.FrontingPreviewRequestV2{
		ResourceID: resources[0].ResourceID, ExpectedCurrentConfigurationRevision: resources[0].CurrentConfigurationRevision,
		RequestedStrategy: protectionfronting.StrategyL4OneToOne,
		SocketClaim: protectionfronting.FrontingSocketClaimReferenceV2{ResourceID: resources[0].ResourceID,
			EndpointID: resources[0].SocketClaims[0].EndpointID, ClaimRevision: resources[0].SocketClaims[0].ClaimRevision},
		BackendReferences: resources[0].BackendReferences, SelectedProxyMode: hostresources.ProxyModeOff,
	}
	plan, err := (&protectionfronting.SemanticServiceV2{Source: fixture.source, Now: func() time.Time { return now }}).Preview(t.Context(), request)
	if err != nil || plan.Strategy.Selected != protectionfronting.StrategyL4OneToOne || plan.Strategy.Actual != protectionfronting.FrontingActualNotAppliedV2 {
		t.Fatalf("production preview=%#v err=%v", plan, err)
	}
	return fixture, request, plan
}

func expectProductionPlanStale(t *testing.T, source *productionFrontingSemanticSource, plan protectionfronting.FrontingStrategyPlanV2, now time.Time) {
	t.Helper()
	if _, err := source.currentFrontingPlanInputV2At(t.Context(), plan, now); err == nil || err.Error() != "plan_stale" {
		t.Fatalf("changed production binding error=%v", err)
	}
}

func TestProductionComposedPrepareAcquiresExactLeaseAndCreatesProtectedArtifactWithoutSwitch(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fixture := newProductionSourceFixture(t, now)
	resources, err := fixture.source.ResourcesV2(t.Context(), now)
	if err != nil || len(resources) != 1 || len(resources[0].SocketClaims) != 1 || len(resources[0].BackendReferences) != 1 {
		t.Fatalf("production facts=%#v err=%v", resources, err)
	}
	request := protectionfronting.FrontingPreviewRequestV2{
		ResourceID: resources[0].ResourceID, ExpectedCurrentConfigurationRevision: resources[0].CurrentConfigurationRevision,
		RequestedStrategy: protectionfronting.StrategyL4OneToOne,
		SocketClaim: protectionfronting.FrontingSocketClaimReferenceV2{ResourceID: resources[0].ResourceID,
			EndpointID: resources[0].SocketClaims[0].EndpointID, ClaimRevision: resources[0].SocketClaims[0].ClaimRevision},
		BackendReferences: resources[0].BackendReferences, SelectedProxyMode: hostresources.ProxyModeOff,
	}
	plan, err := (&protectionfronting.SemanticServiceV2{Source: fixture.source, Now: func() time.Time { return now }}).Preview(t.Context(), request)
	if err != nil || plan.Strategy.Selected != protectionfronting.StrategyL4OneToOne {
		t.Fatalf("production preview=%#v err=%v", plan, err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "fronting-composition.db")), &gorm.Config{})
	if err != nil || protectionrepository.Migrate(db) != nil {
		t.Fatalf("open/migrate workflow DB: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	repository := protectionrepository.New(db)
	manager := protectionoperations.NewManager(repository, protectionoperations.Options{InstanceID: "production-composition-test", PID: 77,
		Audit: func(context.Context, protectionoperations.AuditEvent) error { return nil }})
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	storage, err := protectionartifacts.New(filepath.Join(t.TempDir(), ".runtime", "server-protection"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := protectionhelper.NewManagedRoot(storage.Root())
	if err != nil {
		t.Fatal(err)
	}
	nginx := helperinvoker.NewNginx()
	nginx.ActiveRevision, nginx.ActiveSHA256 = strings.Repeat("a", 64), strings.Repeat("b", 64)
	nginx.Revisions[nginx.ActiveRevision] = nginx.ActiveSHA256
	nginx.RevisionListeners[nginx.ActiveRevision] = []protectionhelper.NginxListener{{Address: plan.PublicSocket.CanonicalBind, Port: int(plan.PublicSocket.PublicPort)}}
	client, err := protectionhelper.NewClient(root, manager, nginx, productionHelperAuditFixture{})
	if err != nil {
		t.Fatal(err)
	}
	workflow := &protectionfronting.Workflow{
		Manager: manager, Helper: client, Artifacts: protectionartifacts.Service{Storage: storage, Store: repository}, Marker: storage, State: storage,
		Recovery: productionBundleFixture{},
		Health: func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
			return []componenthealth.Result{}
		},
		RollbackHealth: func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result {
			return []componenthealth.Result{}
		},
		V2Plans: fixture.source, V2Leases: fixture.source.backends, V2Fallbacks: fixture.source.fallbacks, V2Artifacts: storage,
		V2Health: protectionfronting.NewExactHealthRegistryV2().FixedL4Check(), V2SNIHealth: protectionfronting.NewExactHealthRegistryV2().SNIPrereadCheck(),
		Now: func() time.Time { return now },
	}
	prepared, err := workflow.PrepareV2(t.Context(), protectionfronting.PrepareV2Input{Plan: plan, Actor: "tester", IdempotencyKey: "production-composed-prepare",
		Confirmation: "PREPARE FRONTING " + plan.CanonicalPlanDigest})
	if err != nil || prepared.State != protectionoperations.StatePrepared || prepared.LeaseState != hostresources.EndpointLeaseReserved {
		t.Fatalf("prepared=%#v err=%v", prepared, err)
	}
	fixture.provider.mu.Lock()
	acquire := fixture.provider.acquire
	fixture.provider.mu.Unlock()
	if acquire.ExactReference != plan.Targets.BackendReferences[0] || acquire.HolderID != prepared.OperationID {
		t.Fatalf("lease request=%#v plan target=%#v", acquire, plan.Targets.BackendReferences[0])
	}
	if _, err := os.Stat(filepath.Join(storage.Root(), "operations", prepared.OperationID, "revision.json")); err != nil {
		t.Fatalf("protected artifact pointer missing: %v", err)
	}
	for _, operation := range nginx.Calls {
		if operation == protectionhelper.OperationNginxSwitch || operation == protectionhelper.OperationNginxReload {
			t.Fatalf("prepare invoked active mutation: %s", operation)
		}
	}
	if storage.HasMutationMarker(prepared.OperationID) {
		t.Fatal("prepare wrote a mutation marker")
	}
}

type productionHelperAuditFixture struct{}

func (productionHelperAuditFixture) RecordHelperAudit(context.Context, protectionhelper.AuditEvent) error {
	return nil
}

type productionBundleFixture struct{}

func (productionBundleFixture) CreateBundle(context.Context, protectionrepository.OperationLockModel, string) error {
	return nil
}

type productionSourceFixture struct {
	source   *productionFrontingSemanticSource
	provider *productionBackendProviderFixture
}

func newProductionSourceFixture(t *testing.T, now time.Time) productionSourceFixture {
	t.Helper()
	publicResource, surface := productionPublicResourceFixture(now)
	backendResource := hostresources.ProtectableResource{ID: "core:inbound:17", Kind: "inbound", Owner: "core", Name: "ordinary-backend",
		Protocol: "stream", Listen: "127.0.0.1", Port: 2443, Source: "fixture",
		Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, AcceptsProxyProtocol: hostresources.CapabilityNo,
			OwnerRevision: "core-owner-v1", ConfigRevision: strings.Repeat("8", 64)}}
	backendEndpoint := hostresources.BuildEndpointFact(backendResource, hostresources.NetworkTCP, now)
	backendResource.Endpoints = []hostresources.PublicEndpoint{backendEndpoint}
	fact, err := hostresources.NewFrontingBackendFactV1(hostresources.FrontingBackendFactV1{
		ProviderID: "core", ContributorID: "core", ProviderRevision: "core-fronting-v1",
		HealthRevision: hostresources.Revision("health"), CapacityRevision: hostresources.Revision("capacity"),
		Ownership: hostresources.FrontingBackendProviderManaged, AcceptsProxyProtocol: hostresources.CapabilityNo,
		CanReachManagement: hostresources.CapabilityNo, HealthReady: true, CapacityReady: true,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(),
	}, backendResource, backendEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	provider := &productionBackendProviderFixture{fact: fact, now: now}
	registry := hostresources.NewFrontingBackendRegistryV1()
	if _, err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	runtime := productionReadyRuntimeFixture(t, now, managementExclusionRevisionV2([]hostresources.ProtectableResource{publicResource}))
	snapshot := hostfacts.Snapshot{GeneratedAt: now.Unix(), Facts: []hostfacts.HostSurfaceFactV1{surface}}
	snapshot.OwnerObservationRevision = hostfacts.OwnerObservationSetRevision(snapshot.Facts, []string{"fixture:" + surface.ListenerOwner.ObservationRevision})
	source := &productionFrontingSemanticSource{
		backends: registry, fallbacks: neutralfallback.NewRegistry(), cache: make(map[string]productionFrontingPlanCacheV2),
		resources: func(context.Context) protectionresources.InventorySnapshot {
			return protectionresources.InventorySnapshot{GeneratedAt: now.Unix(), Resources: []hostresources.ProtectableResource{publicResource}}
		},
		surfaces: func() hostfacts.Snapshot { return snapshot },
		runtime: func(context.Context, string, time.Time) (protectionfronting.NginxRuntimeIdentityV2, error) {
			return runtime, nil
		},
	}
	return productionSourceFixture{source: source, provider: provider}
}

type productionBackendProviderFixture struct {
	fact       hostresources.FrontingBackendFactV1
	leaseCalls atomic.Int32
	mu         sync.Mutex
	now        time.Time
	lease      hostresources.EndpointLeaseV1
	acquire    hostresources.AcquireEndpointLeaseRequestV1
}

func (p *productionBackendProviderFixture) ProviderID() string { return "core" }
func (p *productionBackendProviderFixture) FrontingBackendFactsV1(context.Context, time.Time) ([]hostresources.FrontingBackendFactV1, error) {
	return []hostresources.FrontingBackendFactV1{p.fact}, nil
}
func (p *productionBackendProviderFixture) AcquireEndpointLease(_ context.Context, request hostresources.AcquireEndpointLeaseRequestV1) (hostresources.EndpointLeaseV1, error) {
	p.leaseCalls.Add(1)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.acquire = request
	if p.lease.LeaseID != "" {
		return p.lease, nil
	}
	p.lease, _ = hostresources.FinalizeEndpointLeaseV1(hostresources.EndpointLeaseV1{LeaseID: "core-endpoint-fixture", AuthorityProviderID: p.ProviderID(),
		HolderID: request.HolderID, ExactReference: request.ExactReference, State: hostresources.EndpointLeaseReserved,
		IssuedAt: p.now.Unix(), RenewedAt: p.now.Unix(), ExpiresAt: p.now.Add(10 * time.Minute).Unix()})
	return p.lease, nil
}
func (p *productionBackendProviderFixture) FenceEndpointLease(_ context.Context, request hostresources.MutateEndpointLeaseRequestV1) (hostresources.EndpointLeaseV1, error) {
	p.leaseCalls.Add(1)
	return p.transition(request.LeaseID, request.ExpectedRevision, hostresources.EndpointLeaseMutationPending)
}
func (p *productionBackendProviderFixture) ActivateEndpointLease(_ context.Context, request hostresources.MutateEndpointLeaseRequestV1) (hostresources.EndpointLeaseV1, error) {
	p.leaseCalls.Add(1)
	return p.transition(request.LeaseID, request.ExpectedRevision, hostresources.EndpointLeaseActive)
}
func (p *productionBackendProviderFixture) ReleaseEndpointLease(_ context.Context, request hostresources.ReleaseEndpointLeaseRequestV1) (hostresources.EndpointLeaseV1, error) {
	p.leaseCalls.Add(1)
	p.mu.Lock()
	defer p.mu.Unlock()
	if request.LeaseID != p.lease.LeaseID || request.ExpectedRevision != p.lease.LeaseRevision {
		return hostresources.EndpointLeaseV1{}, errors.New("lease stale")
	}
	next := p.lease
	next.State, next.ReleasedAt = hostresources.EndpointLeaseReleased, max(next.RenewedAt, p.now.Unix())
	p.lease, _ = hostresources.FinalizeEndpointLeaseV1(next)
	return p.lease, nil
}
func (p *productionBackendProviderFixture) GetEndpointLease(context.Context, hostresources.GetEndpointLeaseRequestV1) (hostresources.EndpointLeaseV1, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lease.LeaseID == "" {
		return hostresources.EndpointLeaseV1{}, errors.New("missing lease")
	}
	return p.lease, nil
}
func (p *productionBackendProviderFixture) ListEndpointLeases(context.Context, hostresources.ListEndpointLeasesRequestV1) ([]hostresources.EndpointLeaseV1, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lease.LeaseID == "" {
		return []hostresources.EndpointLeaseV1{}, nil
	}
	return []hostresources.EndpointLeaseV1{p.lease}, nil
}

func (p *productionBackendProviderFixture) transition(leaseID, revision string, state hostresources.EndpointLeaseState) (hostresources.EndpointLeaseV1, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if leaseID != p.lease.LeaseID || revision != p.lease.LeaseRevision {
		return hostresources.EndpointLeaseV1{}, errors.New("lease stale")
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

type productionVersionReaderFixture struct {
	observation protectionfronting.NginxVersionObservationV2
}

func (r productionVersionReaderFixture) ReadNginxVersion(context.Context, string) (protectionfronting.NginxVersionObservationV2, error) {
	return r.observation, nil
}

func productionReadyRuntimeFixture(t *testing.T, now time.Time, managementRevision string) protectionfronting.NginxRuntimeIdentityV2 {
	t.Helper()
	root := t.TempDir()
	binary, config := filepath.Join(root, "nginx"), filepath.Join(root, "loader.conf")
	if err := os.WriteFile(binary, []byte("fixture-nginx"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("events {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	supported := protectionfronting.NginxMethodCapabilityV2{Availability: protectionfronting.CapabilitySupportedV2, Revision: strings.Repeat("1", 64)}
	identity, err := (protectionfronting.NginxRuntimeInspectorV2{Config: protectionfronting.NginxRuntimeInspectionConfigV2{
		CandidatePaths: []string{binary}, AllowedExecutableRoots: []string{root}, ManagedRootPath: root, ControlledConfigPath: config,
		InstallationClass: protectionfronting.NginxInstallationManaged, ValidationMethod: supported, ReloadMethod: supported,
		ActiveVerification: supported, ProcessVerification: supported, ListenerVerification: supported,
		ProxyProtocolReceive:          protectionfronting.NginxMethodCapabilityV2{Availability: protectionfronting.CapabilityUnknownV2},
		ProxyProtocolEmit:             protectionfronting.NginxMethodCapabilityV2{Availability: protectionfronting.CapabilityUnknownV2},
		MasterProcessIdentityRevision: strings.Repeat("2", 64), WorkerSetIdentityRevision: strings.Repeat("3", 64),
		ActiveManagedRevision: strings.Repeat("4", 64), HelperProtocolVersion: 1, HelperVersion: "fixture-helper",
		HelperContractVersion: "v1", HelperContractRevision: strings.Repeat("5", 64), ManagementExclusionsRevision: managementRevision,
		ObservedAt: now, ExpiresAt: now.Add(time.Minute),
	}, Reader: productionVersionReaderFixture{observation: protectionfronting.NginxVersionObservationV2{
		Version: "1.27.0", ConfigureArguments: []string{"--with-stream", "--with-stream_ssl_preread_module"},
	}}}).Inspect(t.Context())
	if err != nil || identity.State != protectionfronting.NginxManagedEngineReady {
		t.Fatalf("runtime identity=%#v err=%v", identity, err)
	}
	return identity
}

func productionPublicResourceFixture(now time.Time) (hostresources.ProtectableResource, hostfacts.HostSurfaceFactV1) {
	ownerRevision, configRevision := strings.Repeat("b", 64), strings.Repeat("c", 64)
	expected := hostresources.ExpectedListenerOwnerV1{
		Schema: hostresources.ExpectedListenerOwnerSchemaV1, ContractRevision: strings.Repeat("a", 64),
		InstanceID: "00112233-4455-4677-8899-aabbccddeeff", SourceRevision: "src-" + strings.Repeat("2", 64),
		ArtifactRevision: "art-" + strings.Repeat("3", 64), DeploymentID: "dep-" + strings.Repeat("4", 64),
		RuntimeRootBindingRevision: strings.Repeat("5", 64), ServiceIdentity: "solovey-ui.panel",
		SystemdUnit: "solovey-ui.service", ServiceFragmentPath: "/etc/systemd/system/solovey-ui.service",
		ServiceUnitSHA256: strings.Repeat("7", 64), ServiceControlGroup: "/system.slice/solovey-ui.service",
		ExecutablePath: "/usr/local/bin/solovey-ui", ExecutableSHA256: strings.Repeat("6", 64),
	}
	resource := hostresources.ProtectableResource{ID: "core:inbound:public", Kind: "inbound", Owner: "core", Name: "public-inbound",
		Protocol: "stream", Listen: "192.0.2.20", Port: 443, Public: true, Source: "fixture",
		Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, OwnerRevision: ownerRevision, ConfigRevision: configRevision, ExpectedListenerOwner: expected}}
	endpoint := hostresources.BuildEndpointFact(resource, hostresources.NetworkTCP, now)
	resource.Endpoints = []hostresources.PublicEndpoint{endpoint}
	resource.ListenIntent = hostresources.BuildConfiguredListenIntent(resource)
	pid, parent, session, uid, gid := 100, 1, 100, 0, 0
	process := hostfacts.ProcessFact{PID: &pid, ParentPID: &parent, SessionID: &session, StartTime: "1000", ExeDigest: expected.ExecutableSHA256,
		Executable: expected.ExecutablePath, ExeDevice: 1, ExeInode: 2, UID: &uid, GID: &gid, ControlGroup: expected.ServiceControlGroup}
	service := hostfacts.ServiceFact{SystemdUnit: expected.SystemdUnit, MainPID: &pid, FragmentPath: expected.ServiceFragmentPath,
		FragmentSHA256: expected.ServiceUnitSHA256, ActiveState: "active", SubState: "running", ControlGroup: process.ControlGroup, StartMonotonicUsec: 100}
	owner := hostfacts.ListenerOwnerFactV1{Schema: hostfacts.ListenerOwnerFactSchemaV1,
		Socket: hostfacts.ListenerSocketIdentityV1{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: endpoint.Key.BindAddress,
			Port: endpoint.Key.Port, Inode: "100", Cookie: 101, CoverageFamilies: []hostfacts.Family{hostfacts.FamilyIPv4}},
		Process: process, Service: service,
		Application: hostfacts.ListenerApplicationIdentityV1{InstanceID: expected.InstanceID, SourceRevision: expected.SourceRevision,
			ArtifactRevision: expected.ArtifactRevision, DeploymentID: expected.DeploymentID, OwnerContractRevision: expected.ContractRevision,
			RuntimeRootBindingRevision: expected.RuntimeRootBindingRevision, ExpectedExecutableSHA256: expected.ExecutableSHA256,
			ServiceIdentity: expected.ServiceIdentity, ResourceID: resource.ID, ResourceOwnerRevision: ownerRevision, ConfigurationRevision: configRevision},
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()}
	owner.Seal()
	surface := hostfacts.HostSurfaceFactV1{Schema: hostfacts.SchemaV1, ID: "surface:public", Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4,
		Bind: endpoint.Key.BindAddress, Port: endpoint.Key.Port, Exposure: hostfacts.ExposurePublic, SocketInode: "100", SocketCookie: 101,
		Process: process, Service: service, ListenerOwner: &owner, RegisteredResourceID: resource.ID, DesiredOwner: resource.Owner,
		OwnershipMode: hostfacts.OwnershipManaged, FirstSeen: now.Unix(), LastSeen: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(),
		Source: "fixture", ConfidenceBP: 10000, ConfigurationRevision: configRevision, Classification: hostfacts.ClassificationManagedExact}
	return resource, surface
}
