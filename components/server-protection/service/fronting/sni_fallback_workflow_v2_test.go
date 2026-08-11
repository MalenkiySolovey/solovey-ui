package fronting

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
)

type memoryFallbackProviderV2 struct {
	mu          sync.Mutex
	now         time.Time
	id          string
	target      fallbacktargets.FallbackTargetV2
	reservation fallbacktargets.ProviderTargetReservationV1
	calls       []string
}

func (p *memoryFallbackProviderV2) ProviderID() string { return p.id }
func (p *memoryFallbackProviderV2) InventoryV2(context.Context, fallbacktargets.InventoryV2Request) (fallbacktargets.InventoryV2Result, *fallbacktargets.ProviderContractError) {
	return fallbacktargets.InventoryV2Result{Targets: []fallbacktargets.FallbackTargetV2{p.target}}, nil
}
func (p *memoryFallbackProviderV2) ResolveV2(context.Context, fallbacktargets.FallbackTargetReferenceV2) (fallbacktargets.ResolveV2Result, *fallbacktargets.ProviderContractError) {
	return fallbacktargets.ResolveV2Result{Target: p.target}, nil
}
func (p *memoryFallbackProviderV2) Reserve(_ context.Context, request fallbacktargets.ReserveRequestV1) (fallbacktargets.ReservationResultV1, *fallbacktargets.ProviderContractError) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, "reserve")
	if p.reservation.ReservationID == "" {
		p.reservation = fallbacktargets.ProviderTargetReservationV1{Schema: fallbacktargets.ProviderTargetReservationSchemaV1,
			ReservationID: "fallback-reservation", ReservationRevision: "fallback-reservation-revision-0", HolderID: request.HolderID,
			Purpose: request.Purpose, ExactTargetReference: request.ExactTargetReference, State: fallbacktargets.ReservationReserved,
			IssuedAt: p.now.Unix(), RenewedAt: p.now.Unix(), FreshnessExpiresAt: p.now.Add(5 * time.Minute).Unix()}
	}
	return fallbacktargets.ReservationResultV1{Reservation: p.reservation}, nil
}
func (p *memoryFallbackProviderV2) FenceForMutation(_ context.Context, request fallbacktargets.ReservationMutationRequestV1) (fallbacktargets.ReservationResultV1, *fallbacktargets.ProviderContractError) {
	return p.mutateFallback("fence", request, fallbacktargets.ReservationMutationPending)
}
func (p *memoryFallbackProviderV2) Activate(_ context.Context, request fallbacktargets.ReservationMutationRequestV1) (fallbacktargets.ReservationResultV1, *fallbacktargets.ProviderContractError) {
	return p.mutateFallback("activate", request, fallbacktargets.ReservationActive)
}
func (p *memoryFallbackProviderV2) Renew(_ context.Context, request fallbacktargets.ReservationMutationRequestV1) (fallbacktargets.ReservationResultV1, *fallbacktargets.ProviderContractError) {
	return p.mutateFallback("renew", request, fallbacktargets.ReservationActive)
}
func (p *memoryFallbackProviderV2) mutateFallback(call string, request fallbacktargets.ReservationMutationRequestV1, state fallbacktargets.ReservationState) (fallbacktargets.ReservationResultV1, *fallbacktargets.ProviderContractError) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, call)
	if request.ReservationID != p.reservation.ReservationID || request.ExpectedRevision != p.reservation.ReservationRevision {
		return fallbacktargets.ReservationResultV1{}, &fallbacktargets.ProviderContractError{Class: fallbacktargets.ProviderErrorStale, ReasonCode: "reservation_stale"}
	}
	p.reservation.State = state
	p.reservation.ReservationRevision += "-next"
	p.reservation.RenewedAt++
	p.reservation.FreshnessExpiresAt++
	return fallbacktargets.ReservationResultV1{Reservation: p.reservation}, nil
}
func (p *memoryFallbackProviderV2) Release(_ context.Context, request fallbacktargets.ReleaseReservationRequestV1) (fallbacktargets.ReservationResultV1, *fallbacktargets.ProviderContractError) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, "release")
	if request.ReservationID != p.reservation.ReservationID || request.ExpectedRevision != p.reservation.ReservationRevision {
		return fallbacktargets.ReservationResultV1{}, &fallbacktargets.ProviderContractError{Class: fallbacktargets.ProviderErrorStale, ReasonCode: "reservation_stale"}
	}
	p.reservation.State = fallbacktargets.ReservationReleased
	p.reservation.ReservationRevision += "-next"
	p.reservation.RenewedAt++
	p.reservation.FreshnessExpiresAt++
	p.reservation.ReleasedAt = p.reservation.RenewedAt
	return fallbacktargets.ReservationResultV1{Reservation: p.reservation}, nil
}
func (p *memoryFallbackProviderV2) GetReservation(context.Context, fallbacktargets.GetReservationRequestV1) (fallbacktargets.ReservationResultV1, *fallbacktargets.ProviderContractError) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return fallbacktargets.ReservationResultV1{Reservation: p.reservation}, nil
}
func (p *memoryFallbackProviderV2) ListReservations(_ context.Context, query fallbacktargets.ListReservationsQueryV1) (fallbacktargets.ListReservationsResultV1, *fallbacktargets.ProviderContractError) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reservation.ReservationID == "" || query.HolderID != "" && query.HolderID != p.reservation.HolderID {
		return fallbacktargets.ListReservationsResultV1{}, nil
	}
	return fallbacktargets.ListReservationsResultV1{Reservations: []fallbacktargets.ProviderTargetReservationV1{p.reservation}}, nil
}

func TestSNIWorkflowV2UsesFallbackProviderAuthorityForFixedDefault(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	input := l4PlanInputV2(t, now, true)
	input.DesiredStrategy = StrategySNIPreread
	target := fallbackTargetFixtureV2(t, now)
	reference, err := fallbacktargets.ReferenceV2FromTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	input.FallbackTargets = []FallbackPlanningTargetV2{{Reference: reference, Target: target}}
	input.Selectors, err = CanonicalizeSelectorSetV1([]SelectorRouteInputV1{{SNI: "route.example", TargetReferenceRevision: input.BackendReferences[0].CanonicalReferenceRevision}},
		SelectorDefaultV1{Policy: SelectorDefaultFixedSafe, TargetReferenceRevision: v2Revision(reference)})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanFrontingStrategyV2(input)
	if err != nil || len(plan.Safety.Blocks) != 0 || plan.Strategy.Selected != StrategySNIPreread {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	base := newFrontingFixture(t, passingFrontingHealth)
	endpointProvider := &memoryEndpointLeaseProviderV2{now: now, fail: map[string]bool{}}
	fallbackProvider := &memoryFallbackProviderV2{now: now, id: reference.ProviderID, target: target}
	registry := fallbacktargets.NewRegistry()
	if _, err := registry.RegisterV2(fallbackProvider); err != nil {
		t.Fatal(err)
	}
	source := &fixedPlanSourceV2{input: input}
	base.workflow.Now = func() time.Time { return now }
	base.workflow.V2Plans, base.workflow.V2Leases, base.workflow.V2Fallbacks = source, memoryLeaseDirectoryV2{provider: endpointProvider}, registry
	base.workflow.V2Artifacts = base.storage
	base.workflow.V2Health = func(context.Context, FixedL4HealthRequestV2) (FixedL4HealthEvidenceV2, error) {
		return FixedL4HealthEvidenceV2{}, nil
	}
	base.workflow.V2SNIHealth = passingSNIHealthV2(now)
	prepared, err := base.workflow.PrepareV2(context.Background(), PrepareV2Input{Plan: plan, Actor: "tester", IdempotencyKey: "sni-fallback-default",
		Confirmation: "PREPARE FRONTING " + plan.CanonicalPlanDigest})
	if err != nil || prepared.State != protectionoperations.StatePrepared || len(prepared.TargetAuthorityRevisions) != 2 {
		t.Fatalf("prepared=%#v err=%v", prepared, err)
	}
	applied, err := base.workflow.ApplyV2(context.Background(), ApplyV2Input{OperationID: prepared.OperationID, PlanDigest: plan.CanonicalPlanDigest,
		Confirmation: "APPLY FRONTING " + prepared.OperationID})
	if err != nil || applied.State != protectionoperations.StateApplied {
		t.Fatalf("applied=%#v err=%v", applied, err)
	}
	fallbackProvider.mu.Lock()
	state, calls := fallbackProvider.reservation.State, append([]string(nil), fallbackProvider.calls...)
	fallbackProvider.mu.Unlock()
	if state != fallbacktargets.ReservationActive || strings.Join(calls, ",") != "reserve,fence,activate" {
		t.Fatalf("fallback state=%s calls=%v", state, calls)
	}
	rolled, err := base.workflow.RollbackV2(context.Background(), RollbackV2Input{OperationID: prepared.OperationID, PlanDigest: plan.CanonicalPlanDigest,
		Confirmation: "ROLLBACK FRONTING " + prepared.OperationID})
	if err != nil || rolled.State != protectionoperations.StateRolledBack {
		t.Fatalf("rolled=%#v err=%v", rolled, err)
	}
	fallbackProvider.mu.Lock()
	state, calls = fallbackProvider.reservation.State, append([]string(nil), fallbackProvider.calls...)
	fallbackProvider.mu.Unlock()
	if state != fallbacktargets.ReservationReleased || strings.Join(calls, ",") != "reserve,fence,activate,release" {
		t.Fatalf("fallback state=%s calls=%v", state, calls)
	}
}

func fallbackTargetFixtureV2(t *testing.T, now time.Time) fallbacktargets.FallbackTargetV2 {
	t.Helper()
	latency := uint32(2)
	target, err := fallbacktargets.FinalizeFallbackTargetV2(fallbacktargets.FallbackTargetV2{
		Identity: fallbacktargets.TargetIdentity{ProviderID: "fallback-provider", TargetID: "fallback-target"},
		Publish:  fallbacktargets.PublishFactsV2{Revision: "publish-v1", ContentDigest: strings.Repeat("a", 64)},
		Endpoint: fallbacktargets.EndpointV2{EndpointID: "fallback-endpoint", Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4,
			Address: "127.0.0.1", Port: 18080, Local: true, TransportSecurity: fallbacktargets.TransportSecurityTLS,
			ApplicationProtocols: []fallbacktargets.ApplicationProtocol{fallbacktargets.ApplicationProtocolHTTP2}, AcceptedServerNames: []string{"decoy.example"},
			ProxyProtocol: hostresources.CapabilityNo, CanReachManagement: hostresources.CapabilityNo},
		Health:           fallbacktargets.HealthV2{Readiness: fallbacktargets.ReadinessReady, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), ConnectFirstByteP95MS: &latency},
		Capacity:         fallbacktargets.CapacityV2{State: fallbacktargets.CapacityReady, ReservationSlotsTotal: 4, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		ProviderRevision: "fallback-provider-v1", Source: "provider_inventory", ConfidenceBP: 10_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func passingSNIHealthV2(now time.Time) SNIPrereadHealthCheckV2 {
	return func(_ context.Context, request SNIPrereadHealthRequestV2) (SNIPrereadHealthEvidenceV2, error) {
		probes := make([]SNIHealthProbeEvidenceV2, 0, len(request.Probes))
		for _, probe := range request.Probes {
			evidence := SNIHealthProbeEvidenceV2{ProbeID: probe.ProbeID, ExpectedTargetRevision: probe.ExpectedTargetRevision}
			if probe.ExpectReject {
				evidence.ConnectionRejected = true
			} else {
				evidence.ExpectedBackendReached, evidence.ObservedTargetRevision, evidence.BackendIdentityMarker = true, probe.ExpectedTargetRevision, probe.ExpectedTargetRevision
				evidence.ProxyHeaderObserved = request.ProxyMode == hostresources.ProxyModeOn
			}
			probes = append(probes, evidence)
		}
		return SNIPrereadHealthEvidenceV2{Schema: SNIPrereadHealthSchemaV2, OperationID: request.OperationID, OperationRevision: request.OperationRevision,
			PlanDigest: request.PlanDigest, CandidateRevision: request.CandidateRevision, CandidateSHA256: request.CandidateSHA256,
			SocketClaimRevision: request.SocketClaimRevision, SelectorSetRevision: request.SelectorSetRevision, MapRevision: request.MapRevision,
			UpstreamIDSetRevision: request.UpstreamIDSetRevision, TargetAuthorityRevisions: append([]string(nil), request.TargetAuthorityRevisions...),
			ProxyMode: request.ProxyMode, Probes: probes, ObservedAt: now.Unix(), ExpiresAt: now.Add(20 * time.Second).Unix()}, nil
	}
}
