package resources

import (
	"context"
	"testing"
	"time"
)

func interceptionFactFixture(t *testing.T, now time.Time) InterceptionInboundFactV1 {
	t.Helper()
	digest := Revision("fixture")
	fact, err := FinalizeInterceptionFactV1(InterceptionInboundFactV1{
		ProviderID: "core", ProviderRevision: InterceptionProviderRevisionV1,
		ResourceID: "inbound:7", EndpointID: "endpoint:0123456789abcdef",
		InboundDatabaseID: 7, InboundTag: "redirect-in", Kind: InterceptionRedirectV1,
		Network: NetworkTCP, AddressFamily: AddressFamilyIPv4,
		ConfiguredBind: "0.0.0.0", ConfiguredPort: 15001,
		Ownership: InterceptionProviderManagedV1, ListenerState: InterceptionListenerObservedExactV1,
		ConfigurationRevision: digest, RuntimeRevision: digest, RuntimeGenerationRevision: digest,
		ListenerRevision: digest, CoreSemanticRevision: digest, LinuxOnly: true,
		OriginalDestinationMechanism: "SO_ORIGINAL_DST", OriginalDestinationPreserved: true,
		SourcePreserved: true, RuntimeReady: true,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("finalize fixture: %v", err)
	}
	return fact
}

func TestInterceptionFactRevisionIgnoresObservationClockButExpiryFailsClosed(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	first := interceptionFactFixture(t, now)
	second := first
	second.ObservedAt = now.Add(10 * time.Second).Unix()
	second.ExpiresAt = now.Add(70 * time.Second).Unix()
	second, err := FinalizeInterceptionFactV1(second)
	if err != nil {
		t.Fatalf("refinalize fixture: %v", err)
	}
	if first.FactRevision != second.FactRevision {
		t.Fatal("volatile observation time changed the semantic fact revision")
	}
	if first.Validate(now.Add(61*time.Second)) == nil {
		t.Fatal("expired fact remained valid")
	}
}

func TestInterceptionContractsRejectProtocolAndScopeConfusion(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	redirectUDP := interceptionFactFixture(t, now)
	redirectUDP.Network = NetworkUDP
	if _, err := FinalizeInterceptionFactV1(redirectUDP); err == nil {
		t.Fatal("Redirect UDP was accepted")
	}
	tproxy := interceptionFactFixture(t, now)
	tproxy.Kind = InterceptionTProxyV1
	tproxy.TransparentSocketRequired = true
	tproxy.PolicyRoutingRequired = false
	if _, err := FinalizeInterceptionFactV1(tproxy); err == nil {
		t.Fatal("TProxy without policy routing was accepted")
	}
	if _, err := FinalizeIngressScopeFactV1(ForwardedIngressScopeFactV1{
		ProviderID: "host-network", ProviderRevision: IngressScopeProviderRevisionV1,
		ScopeID: "scope:1", InterfaceName: "lo", InterfaceIndex: 1,
		InterfaceRevision: Revision("lo"), AddressFamily: AddressFamilyIPv4,
		Ownership: IngressScopeProviderManagedV1, ForwardedIngress: true, Loopback: true,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(),
	}); err == nil {
		t.Fatal("loopback was accepted as forwarded ingress")
	}
}

type interceptionProviderFixture struct {
	id   string
	fact InterceptionInboundFactV1
}

func (p interceptionProviderFixture) ProviderID() string { return p.id }
func (p interceptionProviderFixture) InterceptionFactsV1(context.Context, time.Time) ([]InterceptionInboundFactV1, error) {
	return []InterceptionInboundFactV1{p.fact}, nil
}
func (interceptionProviderFixture) AcquireInterceptionLease(context.Context, AcquireInterceptionLeaseRequestV1) (InterceptionLeaseV1, error) {
	return InterceptionLeaseV1{}, nil
}
func (interceptionProviderFixture) FenceInterceptionLease(context.Context, MutateInterceptionLeaseRequestV1) (InterceptionLeaseV1, error) {
	return InterceptionLeaseV1{}, nil
}
func (interceptionProviderFixture) ActivateInterceptionLease(context.Context, MutateInterceptionLeaseRequestV1) (InterceptionLeaseV1, error) {
	return InterceptionLeaseV1{}, nil
}
func (interceptionProviderFixture) RenewInterceptionLease(context.Context, MutateInterceptionLeaseRequestV1) (InterceptionLeaseV1, error) {
	return InterceptionLeaseV1{}, nil
}
func (interceptionProviderFixture) ReleaseInterceptionLease(context.Context, ReleaseInterceptionLeaseRequestV1) (InterceptionLeaseV1, error) {
	return InterceptionLeaseV1{}, nil
}
func (interceptionProviderFixture) GetInterceptionLease(context.Context, GetInterceptionLeaseRequestV1) (InterceptionLeaseV1, error) {
	return InterceptionLeaseV1{}, nil
}
func (interceptionProviderFixture) ListInterceptionLeases(context.Context, ListInterceptionLeasesRequestV1) ([]InterceptionLeaseV1, error) {
	return nil, nil
}

func TestInterceptionRegistryResolvesOnlyExactCurrentRevision(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	fact := interceptionFactFixture(t, now)
	registry := NewInterceptionRegistryV1()
	stop, err := registry.Register(interceptionProviderFixture{id: "core", fact: fact})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer stop()
	reference, err := ReferenceInterceptionV1(fact, now)
	if err != nil {
		t.Fatalf("reference: %v", err)
	}
	if _, err := registry.ResolveV1(context.Background(), reference, now); err != nil {
		t.Fatalf("resolve exact reference: %v", err)
	}
	reference.RuntimeRevision = Revision("stale")
	reference.CanonicalReferenceRevision = Revision(interceptionReferenceRevisionInput(reference))
	if _, err := registry.ResolveV1(context.Background(), reference, now); err == nil {
		t.Fatal("stale runtime reference resolved")
	}
}
