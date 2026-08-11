package fallbacktargets

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestFallbackTargetV2RejectsForgedCanonicalRevision(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	target := validTargetV2(t, "provider", "target", "endpoint", now)
	target.CanonicalTargetRevision = strings.Repeat("b", 64)
	if err := target.Validate(); err == nil {
		t.Fatal("forged canonical target revision accepted")
	}
}

func TestRegistryV2ReturnsCanonicalTargetOrdering(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	target := validTargetV2(t, "provider", "target", "endpoint", now)
	target.Endpoint.ApplicationProtocols[0], target.Endpoint.ApplicationProtocols[1] = target.Endpoint.ApplicationProtocols[1], target.Endpoint.ApplicationProtocols[0]
	target.Endpoint.AcceptedServerNames[0], target.Endpoint.AcceptedServerNames[1] = target.Endpoint.AcceptedServerNames[1], target.Endpoint.AcceptedServerNames[0]
	if err := target.Validate(); err != nil {
		t.Fatalf("semantically canonical target should validate: %v", err)
	}
	registry := NewRegistry()
	if _, err := registry.RegisterV2(&providerV2Stub{id: "provider", inventory: InventoryV2Result{Targets: []FallbackTargetV2{target}}}); err != nil {
		t.Fatal(err)
	}
	snapshot := registry.SnapshotV2(context.Background(), now)
	if len(snapshot.Targets) != 1 || snapshot.Targets[0].Endpoint.ApplicationProtocols[0] != ApplicationProtocolHTTP11 || snapshot.Targets[0].Endpoint.AcceptedServerNames[0] != "example.com" {
		t.Fatalf("registry returned provider ordering instead of canonical ordering: %#v", snapshot.Targets)
	}
}

func TestFallbackTargetV2RejectsUnboundedOrFutureObservations(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	base := validTargetV2(t, "provider", "target", "endpoint", now)
	tooLarge := base
	tooLarge.CanonicalTargetRevision, tooLarge.Endpoint.EndpointRevision, tooLarge.Health.Revision, tooLarge.Capacity.Revision = "", "", "", ""
	tooLarge.Capacity.ReservationSlotsTotal = MaxReservationsV2 + 1
	if _, err := FinalizeFallbackTargetV2(tooLarge); err == nil {
		t.Fatal("capacity above the neutral reservation bound accepted")
	}
	tooLong := base
	tooLong.CanonicalTargetRevision, tooLong.Endpoint.EndpointRevision, tooLong.Health.Revision, tooLong.Capacity.Revision = "", "", "", ""
	tooLong.Health.ExpiresAt = tooLong.Health.ObservedAt + int64((6*time.Minute)/time.Second)
	if _, err := FinalizeFallbackTargetV2(tooLong); err == nil {
		t.Fatal("unbounded health freshness accepted")
	}
	future := base
	future.CanonicalTargetRevision, future.Endpoint.EndpointRevision, future.Health.Revision, future.Capacity.Revision = "", "", "", ""
	future.Health.ObservedAt = now.Add(10 * time.Minute).Unix()
	future.Health.ExpiresAt = now.Add(11 * time.Minute).Unix()
	future.Capacity.ObservedAt = now.Add(10 * time.Minute).Unix()
	future.Capacity.ExpiresAt = now.Add(11 * time.Minute).Unix()
	future, err := FinalizeFallbackTargetV2(future)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := ReferenceV2FromTarget(future)
	if err != nil {
		t.Fatal(err)
	}
	if err := ResolveExactV2(reference, future, now); err == nil {
		t.Fatal("future observation resolved as currently actionable")
	}
}

func TestExpiredCapacityIsExplicitlyStale(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	target := validTargetV2(t, "provider", "target", "endpoint", now)
	if got := EffectiveCapacityStateV2(target.Capacity, now.Add(2*time.Minute)); got != CapacityState("STALE") {
		t.Fatalf("expired capacity collapsed to %q instead of STALE", got)
	}
}

func TestReservationTransitionRejectsProoflessReleaseStaleRenewAndTimeRegression(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	active := reservationFixture(t, now, ReservationActive)
	cas := ReservationCASV1{RequestID: "request", ReservationID: active.ReservationID, ExpectedRevision: active.ReservationRevision}
	if err := ValidateReservationTransition(active, transitionedReservation(active, ReservationReleased), cas, ReservationMutationRelease, now); err == nil {
		t.Fatal("release transition accepted without verified-detachment proof")
	}
	if err := ValidateReservationReleaseTransition(active, transitionedReservation(active, ReservationReleased), ReleaseReservationRequestV1{
		RequestID: "request", ReservationID: active.ReservationID, ExpectedRevision: active.ReservationRevision,
		VerifiedDetachedRevision: strings.Repeat("d", 64),
	}, now); err != nil {
		t.Fatalf("verified release rejected: %v", err)
	}
	expiredNext := transitionedReservation(active, ReservationActive)
	if err := ValidateReservationTransition(active, expiredNext, cas, ReservationMutationRenew, now.Add(10*time.Minute)); err == nil {
		t.Fatal("expired ACTIVE reservation renewed without reconciliation")
	}
	reserved := reservationFixture(t, now, ReservationReserved)
	reservedCAS := ReservationCASV1{RequestID: "request", ReservationID: reserved.ReservationID, ExpectedRevision: reserved.ReservationRevision}
	if err := ValidateReservationTransition(reserved, transitionedReservation(reserved, ReservationReserved), reservedCAS, ReservationMutationRenew, now); err == nil {
		t.Fatal("RESERVED reservation renewed outside the legal matrix")
	}
	active.RenewedAt += 30
	active.FreshnessExpiresAt += 30
	regressed := transitionedReservation(active, ReservationActive)
	regressed.RenewedAt = active.RenewedAt - 1
	if err := ValidateReservationTransition(active, regressed, cas, ReservationMutationRenew, now); err == nil {
		t.Fatal("reservation renewal time regressed")
	}
}

func TestReservationFreshnessBounds(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	reference, err := ReferenceV2FromTarget(validTargetV2(t, "provider", "target", "endpoint", now))
	if err != nil {
		t.Fatal(err)
	}
	request := ReserveRequestV1{RequestID: "request", HolderID: "holder", Purpose: ReservationPurposeNativeFallback, ExactTargetReference: reference, FreshnessDurationSecs: uint32((6 * time.Minute) / time.Second)}
	if err := request.Validate(); err == nil {
		t.Fatal("prepare reservation longer than five minutes accepted")
	}
	reservation := reservationFixture(t, now, ReservationActive)
	reservation.FreshnessExpiresAt = reservation.RenewedAt + int64((16*time.Minute)/time.Second)
	if err := reservation.Validate(); err == nil {
		t.Fatal("ACTIVE reservation longer than the bounded window accepted")
	}
	future := reservationFixture(t, now, ReservationActive)
	future.IssuedAt = now.Add(10 * time.Minute).Unix()
	future.RenewedAt = future.IssuedAt
	future.FreshnessExpiresAt = future.RenewedAt + int64(time.Minute/time.Second)
	if status := future.Status(now); status.EffectiveState != ReservationReconcileRequired || !status.BlocksMutation {
		t.Fatalf("future-dated authority became actionable: %#v", status)
	}
}

func TestReservationReplayCanonicalizesReasonOrdering(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	previous := reservationFixture(t, now, ReservationActive)
	previous.ReasonCodes = []string{"reason-b", "reason-a"}
	replay := previous
	replay.ReasonCodes = []string{"reason-a", "reason-b"}
	if err := ValidateIdempotentReservationReplay(previous, replay); err != nil {
		t.Fatalf("semantic replay rejected because of reason ordering: %v", err)
	}
	previous.ReasonCodes = nil
	replay.ReasonCodes = []string{}
	if err := ValidateIdempotentReservationReplay(previous, replay); err != nil {
		t.Fatalf("semantic replay rejected because of nil versus empty reasons: %v", err)
	}
}

func reservationForProvider(t *testing.T, now time.Time, providerID, reservationID, holder string) ProviderTargetReservationV1 {
	t.Helper()
	reference, err := ReferenceV2FromTarget(validTargetV2(t, providerID, "target", "endpoint", now))
	if err != nil {
		t.Fatal(err)
	}
	return ProviderTargetReservationV1{
		Schema: ProviderTargetReservationSchemaV1, ReservationID: reservationID, ReservationRevision: "revision-1",
		HolderID: holder, Purpose: ReservationPurposeNativeFallback, ExactTargetReference: reference, State: ReservationActive,
		IssuedAt: now.Unix(), RenewedAt: now.Unix(), FreshnessExpiresAt: now.Add(5 * time.Minute).Unix(),
	}
}

func TestRegistryReservationQueryLimitAndFiltersAreEnforced(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	registry := NewRegistry()
	for _, providerID := range []string{"provider-a", "provider-b"} {
		reservation := reservationForProvider(t, now, providerID, "reservation-"+providerID, "other-holder")
		if _, err := registry.RegisterV2(&providerV2Stub{id: providerID, reservations: ListReservationsResultV1{Reservations: []ProviderTargetReservationV1{reservation}}}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := registry.ListReservationsV2(context.Background(), ListReservationsQueryV1{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Reservations) != 1 || !result.Truncated {
		t.Fatalf("global query limit not enforced: count=%d truncated=%v", len(result.Reservations), result.Truncated)
	}
	filtered, err := registry.ListReservationsV2(context.Background(), ListReservationsQueryV1{HolderID: "wanted-holder", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Reservations) != 0 {
		t.Fatalf("provider result bypassed holder filter: %#v", filtered.Reservations)
	}
	if err := (ListReservationsQueryV1{Continuation: "provider-cursor", Limit: 1}).Validate(); err == nil {
		t.Fatal("provider-specific continuation accepted for aggregate query")
	}
}

func TestRegistryReservationProviderPaginationRoundTrips(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	registry := NewRegistry()
	reservation := reservationForProvider(t, now, "provider", "reservation", "holder")
	if _, err := registry.RegisterV2(&providerV2Stub{id: "provider", reservations: ListReservationsResultV1{
		Reservations: []ProviderTargetReservationV1{reservation}, Continuation: "next-page", Truncated: true,
	}}); err != nil {
		t.Fatal(err)
	}
	result, err := registry.ListReservationsV2(context.Background(), ListReservationsQueryV1{ProviderID: "provider", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Continuation != "next-page" || !result.Truncated {
		t.Fatalf("provider continuation was lost: %#v", result)
	}
}

func TestRegistryCancellationStopsProviderDispatch(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	calls := 0
	registry := NewRegistry()
	if _, err := registry.RegisterV2(&providerV2Stub{id: "provider", inventoryCalls: &calls}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	snapshot := registry.SnapshotV2(ctx, now)
	if calls != 0 || !strings.Contains(strings.Join(snapshot.ReasonCodes, ","), "canceled") {
		t.Fatalf("canceled registry dispatched provider calls: calls=%d reasons=%v", calls, snapshot.ReasonCodes)
	}
}

func TestRegistryReasonTruncationIsOrderIndependent(t *testing.T) {
	forward := make([]string, MaxReasonCodesV2+1)
	for index := range forward {
		forward[index] = fmt.Sprintf("reason-%02d", index)
	}
	reverse := append([]string(nil), forward...)
	for left, right := 0, len(reverse)-1; left < right; left, right = left+1, right-1 {
		reverse[left], reverse[right] = reverse[right], reverse[left]
	}
	if left, right := strings.Join(canonicalRegistryReasonsV2(forward), ","), strings.Join(canonicalRegistryReasonsV2(reverse), ","); left != right {
		t.Fatalf("bounded reason set depends on provider order:\n%s\n%s", left, right)
	}
}
