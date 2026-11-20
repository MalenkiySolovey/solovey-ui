package fallbacktargets

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func validTargetV2(t *testing.T, providerID, targetID, endpointID string, now time.Time) FallbackTargetV2 {
	t.Helper()
	latency := uint32(12)
	target, err := FinalizeFallbackTargetV2(FallbackTargetV2{
		Schema:   TargetSchemaV2,
		Identity: TargetIdentity{ProviderID: providerID, TargetID: targetID},
		Publish:  PublishFactsV2{Revision: "publish-1", ContentDigest: strings.Repeat("a", 64)},
		Endpoint: EndpointV2{
			EndpointID: endpointID, Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4,
			Address: "127.0.0.1", Port: 8443, Local: true, TransportSecurity: TransportSecurityTLS,
			ApplicationProtocols: []ApplicationProtocol{ApplicationProtocolHTTP2, ApplicationProtocolHTTP11},
			AcceptedServerNames:  []string{"www.example.com", "example.com"}, ProxyProtocol: hostresources.CapabilityNo,
			CanReachManagement: hostresources.CapabilityNo,
		},
		Health:           HealthV2{Readiness: ReadinessReady, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), ConnectFirstByteP95MS: &latency},
		Capacity:         CapacityV2{State: CapacityReady, ReservationSlotsTotal: 8, ReservationSlotsUsed: 1, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		ProviderRevision: "provider-1", Source: "provider_inventory", ConfidenceBP: 10_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func reservationFixture(t *testing.T, now time.Time, state ReservationState) ProviderTargetReservationV1 {
	t.Helper()
	reference, err := ReferenceV2FromTarget(validTargetV2(t, "provider", "target", "endpoint", now))
	if err != nil {
		t.Fatal(err)
	}
	value := ProviderTargetReservationV1{
		Schema: ProviderTargetReservationSchemaV1, ReservationID: "reservation-1", ReservationRevision: "revision-1",
		HolderID: "operation-1", Purpose: ReservationPurposeNativeFallback, ExactTargetReference: reference, State: state,
		IssuedAt: now.Unix(), RenewedAt: now.Unix(), FreshnessExpiresAt: now.Add(5 * time.Minute).Unix(),
	}
	if state == ReservationReleased {
		value.ReleasedAt = now.Add(time.Minute).Unix()
	}
	return value
}

func transitionedReservation(current ProviderTargetReservationV1, nextState ReservationState) ProviderTargetReservationV1 {
	next := current
	next.State = nextState
	next.ReservationRevision += "-next"
	next.RenewedAt++
	next.FreshnessExpiresAt++
	if nextState == ReservationReleased {
		next.ReleasedAt = next.RenewedAt
	}
	return next
}

type providerV2Stub struct {
	id             string
	inventory      InventoryV2Result
	reservations   ListReservationsResultV1
	err            *ProviderContractError
	wait           bool
	panicCall      bool
	inventoryCalls *int
}

func (provider *providerV2Stub) ProviderID() string { return provider.id }
func (provider *providerV2Stub) InventoryV2(ctx context.Context, _ InventoryV2Request) (InventoryV2Result, *ProviderContractError) {
	if provider.inventoryCalls != nil {
		*provider.inventoryCalls++
	}
	if provider.panicCall {
		panic("provider panic")
	}
	if provider.wait {
		<-ctx.Done()
		return InventoryV2Result{}, &ProviderContractError{Class: ProviderErrorTimeout, ReasonCode: "provider_timeout"}
	}
	return provider.inventory, provider.err
}
func (*providerV2Stub) ResolveV2(context.Context, FallbackTargetReferenceV2) (ResolveV2Result, *ProviderContractError) {
	return ResolveV2Result{}, nil
}
func (*providerV2Stub) Reserve(context.Context, ReserveRequestV1) (ReservationResultV1, *ProviderContractError) {
	return ReservationResultV1{}, nil
}
func (*providerV2Stub) FenceForMutation(context.Context, ReservationMutationRequestV1) (ReservationResultV1, *ProviderContractError) {
	return ReservationResultV1{}, nil
}
func (*providerV2Stub) Activate(context.Context, ReservationMutationRequestV1) (ReservationResultV1, *ProviderContractError) {
	return ReservationResultV1{}, nil
}
func (*providerV2Stub) Renew(context.Context, ReservationMutationRequestV1) (ReservationResultV1, *ProviderContractError) {
	return ReservationResultV1{}, nil
}
func (*providerV2Stub) Release(context.Context, ReleaseReservationRequestV1) (ReservationResultV1, *ProviderContractError) {
	return ReservationResultV1{}, nil
}
func (*providerV2Stub) GetReservation(context.Context, GetReservationRequestV1) (ReservationResultV1, *ProviderContractError) {
	return ReservationResultV1{}, nil
}
func (provider *providerV2Stub) ListReservations(context.Context, ListReservationsQueryV1) (ListReservationsResultV1, *ProviderContractError) {
	return provider.reservations, provider.err
}

func TestRegistryV2DeterministicOrderingDuplicatesAndResolution(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	registry := NewRegistry()
	second := &providerV2Stub{id: "provider-b", inventory: InventoryV2Result{Targets: []FallbackTargetV2{validTargetV2(t, "provider-b", "target-b", "endpoint-b", now)}}}
	first := &providerV2Stub{id: "provider-a", inventory: InventoryV2Result{Targets: []FallbackTargetV2{validTargetV2(t, "provider-a", "target-a", "endpoint-a", now)}}}
	if _, err := registry.RegisterV2(second); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterV2(first); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterV2(&providerV2Stub{id: "provider-a"}); err == nil {
		t.Fatal("duplicate provider registered")
	}
	snapshot := registry.SnapshotV2(context.Background(), now)
	if len(snapshot.Targets) != 2 || snapshot.Targets[0].Identity.ProviderID != "provider-a" {
		t.Fatalf("nondeterministic snapshot: %#v", snapshot)
	}
	reference, _ := ReferenceV2FromTarget(snapshot.Targets[0])
	if _, err := registry.ResolveV2(context.Background(), reference, now); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryV2IsolatesInvalidDuplicateAndPanickingRecords(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	valid := validTargetV2(t, "provider", "target-a", "endpoint-a", now)
	duplicateTarget := valid
	duplicateEndpoint := validTargetV2(t, "provider", "target-b", "endpoint-a", now)
	invalid := validTargetV2(t, "provider", "target-c", "endpoint-c", now)
	invalid.Health.Revision = ""
	registry := NewRegistry()
	_, _ = registry.RegisterV2(&providerV2Stub{id: "provider", inventory: InventoryV2Result{Targets: []FallbackTargetV2{valid, duplicateTarget, duplicateEndpoint, invalid}}})
	_, _ = registry.RegisterV2(&providerV2Stub{id: "panic-provider", panicCall: true})
	snapshot := registry.SnapshotV2(context.Background(), now)
	if len(snapshot.Targets) != 1 {
		t.Fatalf("invalid records not isolated: %#v", snapshot)
	}
	joined := strings.Join(snapshot.ReasonCodes, ",")
	for _, want := range []string{"duplicate_fallback_target_v2_id", "duplicate_fallback_endpoint_v2_id", "fallback_target_record_v2_invalid", "fallback_target_provider_v2_panicked"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing reason %s in %s", want, joined)
		}
	}
}

func TestRegistryV2TimeoutTruncationAndCancellationDoNotLeak(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	registry := NewRegistry()
	_, _ = registry.RegisterV2(&providerV2Stub{id: "provider", wait: true})
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	snapshot := registry.SnapshotV2(ctx, now)
	cancel()
	if len(snapshot.Targets) != 0 || !strings.Contains(strings.Join(snapshot.ReasonCodes, ","), "timeout") {
		t.Fatalf("timeout became empty success: %#v", snapshot)
	}
	time.Sleep(10 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before+1 {
		t.Fatalf("registry leaked goroutines: before=%d after=%d", before, after)
	}
	registry = NewRegistry()
	_, _ = registry.RegisterV2(&providerV2Stub{id: "provider", inventory: InventoryV2Result{Truncated: true}})
	if next := registry.SnapshotV2(context.Background(), now); !next.Truncated || !strings.Contains(strings.Join(next.ReasonCodes, ","), "truncated") {
		t.Fatalf("truncation not explicit: %#v", next)
	}
}

func TestRegistryV2BoundsTargetAndReservationInventories(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	registry := NewRegistry()
	targets := make([]FallbackTargetV2, 0, MaxTargetsV2+1)
	for index := 0; index <= MaxTargetsV2; index++ {
		targets = append(targets, validTargetV2(t, "provider", fmt.Sprintf("target-%d", index), fmt.Sprintf("endpoint-%d", index), now))
	}
	reservations := make([]ProviderTargetReservationV1, MaxReservationListPageV2+1)
	for index := range reservations {
		reservations[index] = reservationFixture(t, now, ReservationActive)
		reservations[index].ReservationID = fmt.Sprintf("reservation-%d", index)
	}
	_, err := registry.RegisterV2(&providerV2Stub{id: "provider", inventory: InventoryV2Result{Targets: targets}, reservations: ListReservationsResultV1{Reservations: reservations}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := registry.SnapshotV2(context.Background(), now)
	if len(snapshot.Targets) != MaxTargetsV2 || !snapshot.Truncated {
		t.Fatalf("target bound failed: count=%d truncated=%v", len(snapshot.Targets), snapshot.Truncated)
	}
	listed, err := registry.ListReservationsV2(context.Background(), ListReservationsQueryV1{Limit: MaxReservationListPageV2})
	if err != nil || len(listed.Reservations) != MaxReservationListPageV2 || !listed.Truncated {
		t.Fatalf("reservation bound failed: count=%d truncated=%v err=%v", len(listed.Reservations), listed.Truncated, err)
	}
}

func TestV1ReadableButNotV2Actionable(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	v1 := readyTarget(now)
	payload, err := json.Marshal(v1)
	if err != nil || len(payload) == 0 {
		t.Fatal("V1 no longer readable")
	}
	var reference FallbackTargetReferenceV2
	if err := json.Unmarshal(payload, &reference); err != nil {
		t.Fatal(err)
	}
	if reference.Validate() == nil {
		t.Fatal("V1 fabricated an actionable V2 reference")
	}
}

func TestNeutralPackageImportBoundary(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		payload, err := os.ReadFile(filepath.Clean(entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(payload))
		for _, forbidden := range []string{"components/fixture-provider", "components/fixture-protection", "service/coreinboundcontrol", "database/"} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s contains forbidden dependency or marker %q", entry.Name(), forbidden)
			}
		}
	}
}

func FuzzFallbackTargetV2Validation(f *testing.F) {
	f.Add("provider", "target", uint16(8443))
	f.Fuzz(func(t *testing.T, providerID, targetID string, port uint16) {
		now := time.Unix(1_000, 0).UTC()
		candidate := validTargetV2(t, "provider", "target", "endpoint", now)
		candidate.Identity.ProviderID, candidate.Identity.TargetID, candidate.Endpoint.Port = providerID, targetID, port
		candidate.CanonicalTargetRevision, candidate.Endpoint.EndpointRevision, candidate.Health.Revision, candidate.Capacity.Revision = "", "", "", ""
		finalized, err := FinalizeFallbackTargetV2(candidate)
		if err == nil && finalized.Validate() != nil {
			t.Fatal("finalized target did not validate")
		}
	})
}

func FuzzFallbackTargetV2CanonicalRevision(f *testing.F) {
	f.Add(true)
	f.Fuzz(func(t *testing.T, reverse bool) {
		now := time.Unix(1_000, 0).UTC()
		candidate := validTargetV2(t, "provider", "target", "endpoint", now)
		candidate.CanonicalTargetRevision, candidate.Endpoint.EndpointRevision, candidate.Health.Revision, candidate.Capacity.Revision = "", "", "", ""
		if reverse {
			sort.Slice(candidate.Endpoint.ApplicationProtocols, func(i, j int) bool {
				return candidate.Endpoint.ApplicationProtocols[i] > candidate.Endpoint.ApplicationProtocols[j]
			})
		}
		first, err := FinalizeFallbackTargetV2(candidate)
		if err != nil {
			t.Fatal(err)
		}
		reverseProtocols := append([]ApplicationProtocol(nil), candidate.Endpoint.ApplicationProtocols...)
		sort.Slice(reverseProtocols, func(i, j int) bool { return reverseProtocols[i] > reverseProtocols[j] })
		candidate.Endpoint.ApplicationProtocols = reverseProtocols
		second, err := FinalizeFallbackTargetV2(candidate)
		if err != nil || first.CanonicalTargetRevision != second.CanonicalTargetRevision {
			t.Fatal("revision was order dependent")
		}
	})
}

func FuzzReservationTransitionValidation(f *testing.F) {
	f.Add(uint8(0), uint8(1))
	f.Fuzz(func(t *testing.T, fromIndex, toIndex uint8) {
		states := []ReservationState{ReservationReserved, ReservationMutationPending, ReservationActive, ReservationReconcileRequired, ReservationReleased}
		now := time.Unix(1_000, 0).UTC()
		current := reservationFixture(t, now, states[int(fromIndex)%len(states)])
		next := transitionedReservation(current, states[int(toIndex)%len(states)])
		cas := ReservationCASV1{RequestID: "request", ReservationID: current.ReservationID, ExpectedRevision: current.ReservationRevision}
		for _, mutation := range []ReservationMutationKind{ReservationMutationFence, ReservationMutationActivate, ReservationMutationRenew, ReservationMutationRelease} {
			_ = ValidateReservationTransition(current, next, cas, mutation, now)
		}
	})
}

func FuzzExactReferenceResolution(f *testing.F) {
	f.Add("publish-1")
	f.Fuzz(func(t *testing.T, publishRevision string) {
		now := time.Unix(1_000, 0).UTC()
		target := validTargetV2(t, "provider", "target", "endpoint", now)
		reference, _ := ReferenceV2FromTarget(target)
		reference.PublishRevision = publishRevision
		err := ResolveExactV2(reference, target, now)
		if publishRevision == target.Publish.Revision && err != nil {
			t.Fatal(err)
		}
		if publishRevision != target.Publish.Revision && err == nil {
			t.Fatal("stale reference resolved")
		}
	})
}
