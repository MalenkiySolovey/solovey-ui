package fallbacktargets

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

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
