package localproxy

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func workflowFactFixture(inboundType string, exposure hostresources.LocalProxyExposureV1, auth hostresources.LocalProxyAuthenticationV1) hostresources.LocalProxyFactV1 {
	now := time.Unix(1_800_000_000, 0).UTC()
	bind := "127.0.0.1"
	if exposure == hostresources.LocalProxyExposurePrivate {
		bind = "10.0.0.8"
	}
	if exposure == hostresources.LocalProxyExposurePublic {
		bind = "203.0.113.8"
	}
	protocols := []hostresources.LocalProxyProtocolV1{hostresources.LocalProxyProtocolSOCKS5}
	if inboundType == "http" {
		protocols = []hostresources.LocalProxyProtocolV1{hostresources.LocalProxyProtocolHTTPConnect, hostresources.LocalProxyProtocolHTTPForward}
	}
	if inboundType == "mixed" {
		protocols = []hostresources.LocalProxyProtocolV1{
			hostresources.LocalProxyProtocolHTTPConnect, hostresources.LocalProxyProtocolHTTPForward, hostresources.LocalProxyProtocolSOCKS5,
		}
	}
	count := 0
	if auth == hostresources.LocalProxyAuthenticationPresent {
		count = 1
	}
	fact := hostresources.LocalProxyFactV1{
		Schema: hostresources.LocalProxyFactSchemaV1, ProviderID: "core", ContributorID: "core",
		ResourceID: "core:inbound:17", EndpointID: "tcp:ipv4:1080", InboundDatabaseID: 17, InboundType: inboundType,
		ConfigurationRevision: hostresources.Revision("config"), EffectiveRuntimeRevision: hostresources.Revision("runtime"),
		RuntimeIdentityRevision: "runtime-owner", ProviderRevision: hostresources.LocalProxyProviderRevisionV1,
		CapabilityRevision:          hostresources.LocalProxyCapabilityRevisionV1,
		ListenerObservationRevision: hostresources.Revision("listener"), OwnerRevision: "owner",
		HealthRevision: hostresources.Revision("health"), CapacityRevision: hostresources.Revision("capacity"),
		ManagementExclusionRevision: hostresources.Revision("management"), RecoveryPathRevision: hostresources.Revision("recovery"),
		ConfiguredBind: bind, ConfiguredPort: 1080, AddressFamily: hostresources.AddressFamilyIPv4,
		ObservedBind: bind, ObservedPort: 1080, ObservedAddressFamily: hostresources.AddressFamilyIPv4,
		Exposure: exposure, Ownership: hostresources.LocalProxyProviderManaged,
		ListenerState: hostresources.LocalProxyListenerObservedExact, Protocols: protocols,
		Authentication: auth, AuthenticationCount: count, AuthenticationRevision: hostresources.Revision("auth"),
		TLS: hostresources.LocalProxyTLSDisabled, TLSRevision: hostresources.Revision("tls"),
		SystemProxy: hostresources.LocalProxySystemProxyDisabled, SystemProxyRevision: hostresources.Revision("system"),
		DependentUDPAssociation: inboundType == "socks" || inboundType == "mixed",
		RuntimeReady:            true, HealthCapabilityReady: true, CapacityReady: true,
		ManagementCollision: hostresources.CapabilityNo, RecoveryPathCollision: hostresources.CapabilityNo,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), ReasonCodes: []string{},
	}
	revisionInput := fact
	revisionInput.ObservedAt, revisionInput.ExpiresAt, revisionInput.FactRevision = 0, 0, ""
	fact.FactRevision = hostresources.Revision(revisionInput)
	return fact
}

func TestLocalProxyPlannerEligibilityMatrixAndHonestMixedProjection(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	tests := []struct {
		name       string
		fact       hostresources.LocalProxyFactV1
		actionable bool
		block      string
		warning    string
	}{
		{"loopback no auth", workflowFactFixture("socks", hostresources.LocalProxyExposureLoopback, hostresources.LocalProxyAuthenticationAbsent), true, "", "LOCAL_ONLY_NO_AUTH_RISK"},
		{"private auth", workflowFactFixture("http", hostresources.LocalProxyExposurePrivate, hostresources.LocalProxyAuthenticationPresent), true, "", "PRIVATE_NETWORK_PROXY_EXPOSURE"},
		{"private no auth", workflowFactFixture("socks", hostresources.LocalProxyExposurePrivate, hostresources.LocalProxyAuthenticationAbsent), false, CodePrivateAuthenticationRequired, ""},
		{"public blocked", workflowFactFixture("http", hostresources.LocalProxyExposurePublic, hostresources.LocalProxyAuthenticationPresent), false, CodeNotShipped, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := planForFact(test.fact, protectionrepository.LocalProxyStateV1Model{}, now)
			if (plan.ApplyGate == ApplyGateExperimentalAck) != test.actionable {
				t.Fatalf("plan=%#v", plan)
			}
			if test.block != "" && !containsLocalProxyCode(plan.BlockCodes, test.block) {
				t.Fatalf("missing block %s: %#v", test.block, plan.BlockCodes)
			}
			if test.warning != "" && !containsLocalProxyCode(plan.WarningCodes, test.warning) {
				t.Fatalf("missing warning %s: %#v", test.warning, plan.WarningCodes)
			}
		})
	}
	mixed := planForFact(workflowFactFixture("mixed", hostresources.LocalProxyExposureLoopback, hostresources.LocalProxyAuthenticationAbsent), protectionrepository.LocalProxyStateV1Model{}, now)
	if mixed.ExactReference == nil || !reflect.DeepEqual(mixed.ExactReference.Protocols, []hostresources.LocalProxyProtocolV1{
		hostresources.LocalProxyProtocolHTTPConnect, hostresources.LocalProxyProtocolHTTPForward, hostresources.LocalProxyProtocolSOCKS5,
	}) || !containsLocalProxyCode(mixed.WarningCodes, "MIXED_ALL_PROTOCOLS_ATOMIC") ||
		!containsLocalProxyCode(mixed.WarningCodes, "SOCKS_UDP_ASSOCIATION_DIAGNOSTICS_ONLY") {
		t.Fatalf("mixed projection=%#v", mixed)
	}
}

func TestLocalProxyPlannerFailsClosedOnOwnerListenerRuntimeAndManagementDrift(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	mutations := map[string]func(*hostresources.LocalProxyFactV1){
		"external owner":       func(f *hostresources.LocalProxyFactV1) { f.Ownership = hostresources.LocalProxyExternalManaged },
		"missing listener":     func(f *hostresources.LocalProxyFactV1) { f.ListenerState = hostresources.LocalProxyListenerUnobserved },
		"runtime drift":        func(f *hostresources.LocalProxyFactV1) { f.RuntimeReady = false },
		"management collision": func(f *hostresources.LocalProxyFactV1) { f.ManagementCollision = hostresources.CapabilityYes },
		"unknown auth": func(f *hostresources.LocalProxyFactV1) {
			f.Authentication, f.AuthenticationCount = hostresources.LocalProxyAuthenticationUnknown, 0
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			fact := workflowFactFixture("socks", hostresources.LocalProxyExposureLoopback, hostresources.LocalProxyAuthenticationAbsent)
			mutate(&fact)
			revisionInput := fact
			revisionInput.ObservedAt, revisionInput.ExpiresAt, revisionInput.FactRevision = 0, 0, ""
			fact.FactRevision = hostresources.Revision(revisionInput)
			plan := planForFact(fact, protectionrepository.LocalProxyStateV1Model{}, now)
			if plan.ApplyGate != ApplyGateBlocked || plan.ExactReference != nil || len(plan.BlockCodes) == 0 {
				t.Fatalf("unsafe plan=%#v", plan)
			}
		})
	}
}

type workflowProbeFixture struct {
	failProtocol hostresources.LocalProxyProtocolV1
	active       atomic.Int32
	maxActive    atomic.Int32
	calls        atomic.Int32
}

func (*workflowProbeFixture) ProviderID() string       { return "core-sing-box-local-proxy-probe-v1" }
func (*workflowProbeFixture) ProviderInstance() string { return "workflow-probe-instance" }
func (p *workflowProbeFixture) Capability(_ context.Context, target componenthealth.LocalProxyProbeTargetV1) componenthealth.LocalProxyProbeCapabilityV1 {
	return componenthealth.FinalizeLocalProxyProbeCapabilityV1(componenthealth.LocalProxyProbeCapabilityV1{
		ProviderID: p.ProviderID(), ProviderInstance: p.ProviderInstance(), ResourceID: target.ResourceID,
		EndpointID: target.EndpointID, Protocol: target.Protocol, Available: true,
	})
}
func (p *workflowProbeFixture) Probe(_ context.Context, request componenthealth.LocalProxyProbeRequestV1) (componenthealth.LocalProxyProbeObservationV1, error) {
	active := p.active.Add(1)
	for current := p.maxActive.Load(); active > current && !p.maxActive.CompareAndSwap(current, active); current = p.maxActive.Load() {
	}
	defer p.active.Add(-1)
	p.calls.Add(1)
	started := time.Now().UTC().UnixNano()
	if started <= request.NotBeforeUnixNano {
		started = request.NotBeforeUnixNano + 1
	}
	target := request.Target
	probeID := hostresources.Revision(struct {
		Challenge string
		Protocol  hostresources.LocalProxyProtocolV1
	}{request.ChallengeRevision, target.Protocol})
	sink := hostresources.Revision(struct{ Challenge string }{request.ChallengeRevision})
	passed := target.Protocol != p.failProtocol
	reasons := []string{}
	if !passed {
		reasons = []string{"PROTOCOL_TRANSACTION_FAILED"}
	}
	return componenthealth.FinalizeLocalProxyProbeObservationV1(componenthealth.LocalProxyProbeObservationV1{
		ProviderID: p.ProviderID(), ProviderInstance: p.ProviderInstance(), ResourceID: target.ResourceID,
		EndpointID: target.EndpointID, Protocol: target.Protocol, ConfigurationRevision: target.ConfigurationRevision,
		RuntimeRevision: target.RuntimeRevision, FactRevision: target.FactRevision,
		ListenerObservationRevision: target.ListenerObservationRevision, AuthenticationRevision: target.AuthenticationRevision,
		TLSRevision: target.TLSRevision, SystemProxyRevision: target.SystemProxyRevision,
		LeaseID: target.LeaseID, LeaseRevision: target.LeaseRevision, LeaseState: target.LeaseState,
		OperationID: target.OperationID, OperationRevision: target.OperationRevision,
		PlanRevision: target.PlanRevision, MarkerRevision: target.MarkerRevision,
		ChallengeRevision: request.ChallengeRevision, Generation: request.MinimumGeneration, ProbeID: probeID,
		StartedUnixNano: started, CompletedUnixNano: started, ExpiresUnixNano: started + int64(30*time.Second),
		Passed: passed, PositiveTransaction: passed, MissingAuthenticationDenied: true,
		InvalidAuthenticationDenied: true, ExactTarget: true, ExactSink: true, SinkRevision: sink,
		ResponderRevision: hostresources.Revision(struct{ Challenge, Probe, Sink string }{request.ChallengeRevision, probeID, sink}),
		ReasonCodes:       reasons,
	}), nil
}

func TestMixedHealthVerifiesEveryProtocolSequentiallyAndFailsAtomically(t *testing.T) {
	now := time.Now().UTC()
	fact := workflowFactFixture("mixed", hostresources.LocalProxyExposureLoopback, hostresources.LocalProxyAuthenticationAbsent)
	fact.ObservedAt, fact.ExpiresAt = now.Add(-time.Second).Unix(), now.Add(time.Minute).Unix()
	revisionInput := fact
	revisionInput.ObservedAt, revisionInput.ExpiresAt, revisionInput.FactRevision = 0, 0, ""
	fact.FactRevision = hostresources.Revision(revisionInput)
	plan := planForFact(fact, protectionrepository.LocalProxyStateV1Model{}, now)
	operation := protectionrepository.OperationLockModel{OperationID: "operation-1", Revision: 3}
	lease, err := hostresources.FinalizeLocalProxyGuardLeaseV1(hostresources.LocalProxyGuardLeaseV1{
		LeaseID: "lease-1", AuthorityProviderID: plan.ExactReference.ProviderID, HolderID: operation.OperationID,
		ExactReference: *plan.ExactReference, State: hostresources.EndpointLeaseMutationPending,
		IssuedAt: now.Unix(), RenewedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := &workflowProbeFixture{}
	probes := componenthealth.NewLocalProxyProbeRegistryV1()
	if _, err := probes.Register(provider); err != nil {
		t.Fatal(err)
	}
	controller := &Controller{Probes: probes}
	health, err := controller.probeAll(t.Context(), plan, operation, lease, hostresources.Revision("marker"), now.Add(-time.Millisecond).UnixNano())
	if err != nil || len(health) != len(plan.ExactReference.Protocols) || provider.calls.Load() != int32(len(plan.ExactReference.Protocols)) {
		t.Fatalf("health=%#v calls=%d err=%v", health, provider.calls.Load(), err)
	}
	if provider.maxActive.Load() != 1 {
		t.Fatalf("probe concurrency=%d, want bounded sequential execution", provider.maxActive.Load())
	}
	failing := &workflowProbeFixture{failProtocol: hostresources.LocalProxyProtocolSOCKS5}
	failingRegistry := componenthealth.NewLocalProxyProbeRegistryV1()
	if _, err := failingRegistry.Register(failing); err != nil {
		t.Fatal(err)
	}
	controller.Probes = failingRegistry
	partial, err := controller.probeAll(t.Context(), plan, operation, lease, hostresources.Revision("marker-2"), now.Add(-time.Millisecond).UnixNano())
	if err == nil || len(partial) >= len(plan.ExactReference.Protocols) {
		t.Fatalf("partial mixed health was accepted: health=%#v err=%v", partial, err)
	}
	expired := lease
	expired.IssuedAt = now.Add(-2 * time.Minute).Unix()
	expired.RenewedAt = expired.IssuedAt
	expired.ExpiresAt = now.Add(-time.Second).Unix()
	expired, err = hostresources.FinalizeLocalProxyGuardLeaseV1(expired)
	if err != nil {
		t.Fatal(err)
	}
	callsBefore := failing.calls.Load()
	if _, err := controller.probeAll(t.Context(), plan, operation, expired, hostresources.Revision("marker-expired"), now.Add(-time.Millisecond).UnixNano()); err == nil ||
		failing.calls.Load() != callsBefore {
		t.Fatalf("expired lease reached a protocol probe: calls=%d before=%d err=%v", failing.calls.Load(), callsBefore, err)
	}
}

type workflowProviderFixture struct {
	mu        sync.Mutex
	now       time.Time
	fact      hostresources.LocalProxyFactV1
	lease     hostresources.LocalProxyGuardLeaseV1
	acquires  atomic.Int32
	fences    atomic.Int32
	activates atomic.Int32
	renews    atomic.Int32
	releases  atomic.Int32
}

func (*workflowProviderFixture) ProviderID() string { return "core" }

func (p *workflowProviderFixture) LocalProxyFactsV1(context.Context, time.Time) ([]hostresources.LocalProxyFactV1, error) {
	return []hostresources.LocalProxyFactV1{p.fact}, nil
}

func (p *workflowProviderFixture) AcquireLocalProxyGuardLease(_ context.Context, request hostresources.AcquireLocalProxyGuardLeaseRequestV1) (hostresources.LocalProxyGuardLeaseV1, error) {
	if err := request.Validate(); err != nil {
		return hostresources.LocalProxyGuardLeaseV1{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.acquires.Add(1)
	lease, err := hostresources.FinalizeLocalProxyGuardLeaseV1(hostresources.LocalProxyGuardLeaseV1{
		LeaseID: "local-proxy-lease-1", AuthorityProviderID: p.ProviderID(), HolderID: request.HolderID,
		ExactReference: request.ExactReference, State: hostresources.EndpointLeaseReserved,
		IssuedAt: p.now.Unix(), RenewedAt: p.now.Unix(),
		ExpiresAt: p.now.Add(time.Duration(request.FreshnessSeconds) * time.Second).Unix(),
	})
	if err == nil {
		p.lease = lease
	}
	return lease, err
}

func (p *workflowProviderFixture) FenceLocalProxyGuardLease(_ context.Context, request hostresources.MutateLocalProxyGuardLeaseRequestV1) (hostresources.LocalProxyGuardLeaseV1, error) {
	p.fences.Add(1)
	return p.mutate(request.LeaseID, request.ExpectedRevision, hostresources.EndpointLeaseMutationPending, 0)
}

func (p *workflowProviderFixture) ActivateLocalProxyGuardLease(_ context.Context, request hostresources.MutateLocalProxyGuardLeaseRequestV1) (hostresources.LocalProxyGuardLeaseV1, error) {
	p.activates.Add(1)
	return p.mutate(request.LeaseID, request.ExpectedRevision, hostresources.EndpointLeaseActive, 0)
}

func (p *workflowProviderFixture) RenewLocalProxyGuardLease(_ context.Context, request hostresources.MutateLocalProxyGuardLeaseRequestV1) (hostresources.LocalProxyGuardLeaseV1, error) {
	p.renews.Add(1)
	return p.mutate(request.LeaseID, request.ExpectedRevision, hostresources.EndpointLeaseActive, request.FreshnessSeconds)
}

func (p *workflowProviderFixture) ReleaseLocalProxyGuardLease(_ context.Context, request hostresources.ReleaseLocalProxyGuardLeaseRequestV1) (hostresources.LocalProxyGuardLeaseV1, error) {
	if err := request.Validate(); err != nil {
		return hostresources.LocalProxyGuardLeaseV1{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.releases.Add(1)
	if p.lease.LeaseID != request.LeaseID || p.lease.LeaseRevision != request.ExpectedRevision {
		return hostresources.LocalProxyGuardLeaseV1{}, hostresources.ErrLocalProxyGuardLeaseConflictV1
	}
	next := p.lease
	next.State = hostresources.EndpointLeaseReleased
	next.ReleasedAt = p.now.Add(time.Second).Unix()
	finalized, err := hostresources.FinalizeLocalProxyGuardLeaseV1(next)
	if err == nil {
		p.lease = finalized
	}
	return finalized, err
}

func (p *workflowProviderFixture) GetLocalProxyGuardLease(_ context.Context, request hostresources.GetLocalProxyGuardLeaseRequestV1) (hostresources.LocalProxyGuardLeaseV1, error) {
	if err := request.Validate(); err != nil {
		return hostresources.LocalProxyGuardLeaseV1{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lease.LeaseID != request.LeaseID {
		return hostresources.LocalProxyGuardLeaseV1{}, hostresources.ErrLocalProxyGuardLeaseConflictV1
	}
	return p.lease, nil
}

func (p *workflowProviderFixture) ListLocalProxyGuardLeases(_ context.Context, request hostresources.ListLocalProxyGuardLeasesRequestV1) ([]hostresources.LocalProxyGuardLeaseV1, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lease.HolderID != request.HolderID {
		return []hostresources.LocalProxyGuardLeaseV1{}, nil
	}
	return []hostresources.LocalProxyGuardLeaseV1{p.lease}, nil
}

func (p *workflowProviderFixture) mutate(leaseID, revision string, state hostresources.EndpointLeaseState, freshness uint32) (hostresources.LocalProxyGuardLeaseV1, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lease.LeaseID != leaseID || p.lease.LeaseRevision != revision {
		return hostresources.LocalProxyGuardLeaseV1{}, hostresources.ErrLocalProxyGuardLeaseConflictV1
	}
	next := p.lease
	next.State = state
	if freshness > 0 {
		next.RenewedAt = p.now.Add(time.Second).Unix()
		next.ExpiresAt = p.now.Add(time.Second + time.Duration(freshness)*time.Second).Unix()
	}
	finalized, err := hostresources.FinalizeLocalProxyGuardLeaseV1(next)
	if err == nil {
		p.lease = finalized
	}
	return finalized, err
}

func TestPreviewIsZeroWritePrepareOnlyReservesAndRestartCancelsPrepared(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "local-proxy-workflow.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&protectionrepository.OperationLockModel{},
		&protectionrepository.LocalProxyStateV1Model{},
		&protectionrepository.LocalProxyIdempotencyV1Model{},
	); err != nil {
		t.Fatal(err)
	}
	repository := protectionrepository.New(db)
	manager := protectionoperations.NewManager(repository, protectionoperations.Options{
		InstanceID: "local-proxy-workflow-test", PID: 4242, Now: func() time.Time { return now },
		Lease: 10 * time.Minute,
	})
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })

	fact := workflowFactFixture("mixed", hostresources.LocalProxyExposureLoopback, hostresources.LocalProxyAuthenticationAbsent)
	provider := &workflowProviderFixture{now: now, fact: fact}
	providers := hostresources.NewLocalProxyRegistryV1()
	if _, err := providers.Register(provider); err != nil {
		t.Fatal(err)
	}
	controller := &Controller{
		Repository: repository, Operations: manager, Providers: providers,
		Probes: componenthealth.NewLocalProxyProbeRegistryV1(), Now: func() time.Time { return now },
	}
	reference := PlanReferenceV1{ResourceID: fact.ResourceID, EndpointID: fact.EndpointID, FactRevision: fact.FactRevision}
	plan, err := controller.Preview(t.Context(), reference)
	if err != nil {
		t.Fatal(err)
	}
	for name, model := range map[string]any{
		"operation": &protectionrepository.OperationLockModel{},
		"state":     &protectionrepository.LocalProxyStateV1Model{},
		"receipt":   &protectionrepository.LocalProxyIdempotencyV1Model{},
	} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("preview wrote %s rows: count=%d err=%v", name, count, err)
		}
	}
	if provider.acquires.Load()+provider.fences.Load()+provider.activates.Load()+provider.renews.Load()+provider.releases.Load() != 0 {
		t.Fatal("preview invoked a lease mutation")
	}

	prepareInput := PrepareRequestV1{
		ResourceID: fact.ResourceID, EndpointID: fact.EndpointID, FactRevision: fact.FactRevision,
		PlanID: plan.PlanID, PlanDigest: plan.PlanDigest, IdempotencyKey: "prepare-workflow-1",
		Acknowledged: true, Confirmation: "PREPARE LOCAL PROXY " + plan.PlanID,
	}
	prepared, err := controller.Prepare(t.Context(), "admin:test", prepareInput)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.ActualState != StatePrepared || prepared.Lease.State != hostresources.EndpointLeaseReserved ||
		provider.acquires.Load() != 1 || provider.fences.Load() != 0 || provider.activates.Load() != 0 ||
		provider.renews.Load() != 0 || provider.releases.Load() != 0 {
		t.Fatalf("prepare crossed the mutation boundary: result=%#v counters=%d/%d/%d/%d/%d",
			prepared, provider.acquires.Load(), provider.fences.Load(), provider.activates.Load(), provider.renews.Load(), provider.releases.Load())
	}
	for name, model := range map[string]any{
		"operation": &protectionrepository.OperationLockModel{},
		"state":     &protectionrepository.LocalProxyStateV1Model{},
		"receipt":   &protectionrepository.LocalProxyIdempotencyV1Model{},
	} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("prepare %s rows: count=%d err=%v", name, count, err)
		}
	}
	operation, err := repository.OperationByID(t.Context(), prepared.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	preparedState, err := repository.LocalProxyStateByOperation(t.Context(), operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	preparedLease := provider.lease
	provider.mu.Unlock()
	corruptState := preparedState
	corruptState.ReferenceRevision = hostresources.Revision("unrelated-reference")
	if err := validateStoredLocalProxyBinding(plan, corruptState, preparedLease, false); err == nil {
		t.Fatal("corrupt mirror reference was accepted")
	}
	wrongHolder := preparedLease
	wrongHolder.HolderID = "operation-unrelated"
	wrongHolder, err = hostresources.FinalizeLocalProxyGuardLeaseV1(wrongHolder)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateStoredLocalProxyBinding(plan, preparedState, wrongHolder, true); err == nil {
		t.Fatal("provider drift mode accepted a lease owned by another operation")
	}
	decision, err := (Reconciler{Controller: controller}).Reconcile(t.Context(), operation)
	if err != nil {
		t.Fatal(err)
	}
	if decision.State != protectionoperations.StateCancelled || provider.releases.Load() != 1 ||
		provider.fences.Load() != 0 || provider.activates.Load() != 0 || provider.renews.Load() != 0 {
		t.Fatalf("restart promoted prepared authority: decision=%#v counters=%d/%d/%d/%d",
			decision, provider.releases.Load(), provider.fences.Load(), provider.activates.Load(), provider.renews.Load())
	}
	state, err := repository.LocalProxyStateByOperation(t.Context(), operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if state.ActualState != string(StateNotApplied) || state.GuardingProviderLease || state.RecoveryRequired ||
		state.LeaseState != string(hostresources.EndpointLeaseReleased) {
		t.Fatalf("restart state=%#v", state)
	}
	if err := controller.markRenewalRecovery(t.Context(), preparedState); err != nil {
		t.Fatal(err)
	}
	state, err = repository.LocalProxyStateByOperation(t.Context(), operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if state.ActualState != string(StateNotApplied) || state.RecoveryRequired || state.GuardingProviderLease {
		t.Fatalf("stale renewal recovery overwrote released authority: %#v", state)
	}

	provider.fact.ConfigurationRevision = hostresources.Revision("advanced-configuration")
	provider.fact.FactRevision = hostresources.Revision(func() hostresources.LocalProxyFactV1 {
		value := provider.fact
		value.FactRevision, value.ObservedAt, value.ExpiresAt = "", 0, 0
		return value
	}())
	prepareReplay, err := controller.Prepare(t.Context(), "admin:test", prepareInput)
	if err != nil || !prepareReplay.Replayed || prepareReplay.OperationID != prepared.OperationID {
		t.Fatalf("completed prepare did not replay after fact drift: result=%#v err=%v", prepareReplay, err)
	}
	for action, request := range map[string]any{
		"apply": ApplyRequestV1{
			OperationID: "operation-completed-apply", OperationRevision: 9, PlanID: plan.PlanID,
			PlanDigest: plan.PlanDigest, FactRevision: plan.FactRevision, IdempotencyKey: "apply-replay-1",
			Acknowledged: true, Confirmation: "APPLY LOCAL PROXY operation-completed-apply",
		},
		"disable": DisableRequestV1{
			OperationID: "operation-completed-disable", OperationRevision: 10, IdempotencyKey: "disable-replay-1",
			Confirmation: "DISABLE LOCAL PROXY operation-completed-disable",
		},
	} {
		requestDigest := hostresources.Revision(request)
		receipt, replay, err := repository.BeginLocalProxyReceipt(t.Context(), action, action+"-replay-1", requestDigest)
		if err != nil || replay {
			t.Fatalf("%s receipt=%#v replay=%v err=%v", action, receipt, replay, err)
		}
		expected := ResultV1{OperationID: "operation-completed-" + action, OperationRevision: 11, ActualState: StateNotApplied}
		if err := repository.CompleteLocalProxyReceipt(t.Context(), receipt.ID, expected.OperationID, expected.OperationRevision, expected); err != nil {
			t.Fatal(err)
		}
		var replayed ResultV1
		switch typed := request.(type) {
		case ApplyRequestV1:
			replayed, err = controller.Apply(t.Context(), typed)
		case DisableRequestV1:
			replayed, err = controller.Disable(t.Context(), typed)
		}
		if err != nil || !replayed.Replayed || replayed.OperationID != expected.OperationID {
			t.Fatalf("%s terminal retry did not replay: result=%#v err=%v", action, replayed, err)
		}
	}
	if _, err := manager.Transition(t.Context(), operation.OperationID, operation.Revision, protectionoperations.StateCancelled); err != nil {
		t.Fatal(err)
	}
}

func TestRestartDistrustsActiveHistoryAndRetainsFreshMarkerOnHealthFailure(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "local-proxy-restart.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&protectionrepository.OperationLockModel{},
		&protectionrepository.LocalProxyStateV1Model{},
		&protectionrepository.LocalProxyIdempotencyV1Model{},
	); err != nil {
		t.Fatal(err)
	}
	repository := protectionrepository.New(db)
	manager := protectionoperations.NewManager(repository, protectionoperations.Options{
		InstanceID: "local-proxy-restart-test", PID: 4243, Now: func() time.Time { return now },
		Lease: 10 * time.Minute,
	})
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	fact := workflowFactFixture("mixed", hostresources.LocalProxyExposureLoopback, hostresources.LocalProxyAuthenticationAbsent)
	provider := &workflowProviderFixture{now: now, fact: fact}
	providers := hostresources.NewLocalProxyRegistryV1()
	if _, err := providers.Register(provider); err != nil {
		t.Fatal(err)
	}
	failingProbe := &workflowProbeFixture{failProtocol: hostresources.LocalProxyProtocolSOCKS5}
	probes := componenthealth.NewLocalProxyProbeRegistryV1()
	if _, err := probes.Register(failingProbe); err != nil {
		t.Fatal(err)
	}
	controller := &Controller{
		Repository: repository, Operations: manager, Providers: providers, Probes: probes,
		Now: func() time.Time { return now },
	}
	plan, err := controller.Preview(t.Context(), PlanReferenceV1{
		ResourceID: fact.ResourceID, EndpointID: fact.EndpointID, FactRevision: fact.FactRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := controller.Prepare(t.Context(), "admin:test", PrepareRequestV1{
		ResourceID: fact.ResourceID, EndpointID: fact.EndpointID, FactRevision: fact.FactRevision,
		PlanID: plan.PlanID, PlanDigest: plan.PlanDigest, IdempotencyKey: "prepare-restart-1",
		Acknowledged: true, Confirmation: "PREPARE LOCAL PROXY " + plan.PlanID,
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := repository.OperationByID(t.Context(), prepared.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	provider.mu.Lock()
	reserved := provider.lease
	provider.mu.Unlock()
	fenced, err := provider.FenceLocalProxyGuardLease(t.Context(), hostresources.MutateLocalProxyGuardLeaseRequestV1{
		RequestID: hostresources.Revision("restart-fence"), LeaseID: reserved.LeaseID, ExpectedRevision: reserved.LeaseRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err := provider.ActivateLocalProxyGuardLease(t.Context(), hostresources.MutateLocalProxyGuardLeaseRequestV1{
		RequestID: hostresources.Revision("restart-activate"), LeaseID: fenced.LeaseID, ExpectedRevision: fenced.LeaseRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.persistState(t.Context(), plan, operation, active, StateAppliedExperimental, "historical-marker", nil, false); err != nil {
		t.Fatal(err)
	}
	decision, err := (Reconciler{Controller: controller}).Reconcile(t.Context(), operation)
	if err != nil {
		t.Fatal(err)
	}
	if decision.State != protectionoperations.StateReconcileRequired || failingProbe.calls.Load() < 1 ||
		failingProbe.calls.Load() > int32(len(plan.ExactReference.Protocols)) {
		t.Fatalf("restart decision=%#v probe calls=%d", decision, failingProbe.calls.Load())
	}
	state, err := repository.LocalProxyStateByOperation(t.Context(), operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if state.ActualState != string(StateRecoveryRequired) || !state.RecoveryRequired || !state.GuardingProviderLease ||
		state.MarkerRevision == "" || state.MarkerRevision == "historical-marker" ||
		state.LeaseState != string(hostresources.EndpointLeaseActive) || provider.releases.Load() != 0 ||
		provider.renews.Load() != 0 {
		t.Fatalf("restart failure did not retain exact guarded recovery state: %#v", state)
	}
}

func TestLocalProxyPlanHighCardinalityIsBoundedDeterministicAndFast(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	fact := workflowFactFixture("mixed", hostresources.LocalProxyExposureLoopback, hostresources.LocalProxyAuthenticationAbsent)
	const count = 2048
	start := time.Now()
	var firstDigest string
	for index := 0; index < count; index++ {
		plan := planForFact(fact, protectionrepository.LocalProxyStateV1Model{}, now)
		if index == 0 {
			firstDigest = plan.PlanDigest
		} else if plan.PlanDigest != firstDigest {
			t.Fatal("deterministic input produced different plan digest")
		}
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("local proxy control planning exceeded 2s budget for %d facts: %s", count, elapsed)
	}
	allocations := testing.AllocsPerRun(5, func() {
		_ = planForFact(fact, protectionrepository.LocalProxyStateV1Model{}, now)
	})
	if allocations > 250 {
		t.Fatalf("plan allocations %.1f exceed bounded control-plane budget", allocations)
	}
}

func BenchmarkLocalProxyPlanV1(b *testing.B) {
	now := time.Unix(1_800_000_000, 0).UTC()
	fact := workflowFactFixture("mixed", hostresources.LocalProxyExposureLoopback, hostresources.LocalProxyAuthenticationAbsent)
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		_ = planForFact(fact, protectionrepository.LocalProxyStateV1Model{}, now)
	}
}

func containsLocalProxyCode(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(value, wanted) {
			return true
		}
	}
	return false
}
