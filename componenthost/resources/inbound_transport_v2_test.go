package resources

import (
	"context"
	"testing"
	"time"
)

type transportProviderFixture struct {
	id    string
	facts []InboundTransportCapabilityV2
}

func (p transportProviderFixture) ProviderID() string { return p.id }
func (p transportProviderFixture) InboundTransportCapabilitiesV2(context.Context, time.Time) ([]InboundTransportCapabilityV2, error) {
	return p.facts, nil
}

func validTransportFact(now time.Time, resource string) InboundTransportCapabilityV2 {
	digest := Revision(resource)
	feature := FinalizeRuntimeBuildFeatureV1(RuntimeBuildFeatureV1{Feature: "with_quic", State: RuntimeFeatureSupported, RuntimeIdentity: digest, SourceRevision: digest, ModuleRevision: digest, BuildProfileRevision: digest, ObservationMethod: "compile_tag", ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()})
	return FinalizeInboundTransportCapabilityV2(InboundTransportCapabilityV2{ProviderID: "provider", ContributorID: "contributor", ResourceID: resource, InboundDatabaseID: 1, InboundType: "direct", StrategyClass: TransportPlainUDP, ConfigurationRevision: digest, EffectiveRuntimeRevision: digest, PinnedRuntimeIdentity: digest, BuildFeature: feature, EffectiveNetworks: []Network{NetworkUDP}, EffectiveNetworksRevision: digest, TransportRevision: digest, SocketIntentRevision: digest, AuthenticationRevision: digest, TLSSemanticRevision: digest, UDPTimeoutRevision: digest, ListenerOwnerRevision: digest, RuntimeGenerationRevision: digest, ActionableDirectUDPSocket: true, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()})
}

func TestInboundTransportRegistryDeterministicBoundedAndFailClosed(t *testing.T) {
	now := time.Unix(1000, 0)
	first := validTransportFact(now, "core:inbound:2")
	second := validTransportFact(now, "core:inbound:1")
	registry := NewInboundTransportRegistryV2()
	unregister, err := registry.Register(transportProviderFixture{id: "provider", facts: []InboundTransportCapabilityV2{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := registry.Facts(context.Background(), now)
	if err != nil || len(facts) != 2 || facts[0].ResourceID != "core:inbound:1" {
		t.Fatalf("facts=%#v err=%v", facts, err)
	}
	unregister()
	facts, err = registry.Facts(context.Background(), now)
	if err != nil || len(facts) != 0 {
		t.Fatalf("post unregister=%#v %v", facts, err)
	}
	bad := validTransportFact(now, "core:inbound:bad")
	bad.ActionableDirectUDPSocket = true
	bad.EffectiveNetworks = []Network{NetworkTCP}
	bad = FinalizeInboundTransportCapabilityV2(bad)
	registry = NewInboundTransportRegistryV2()
	_, _ = registry.Register(transportProviderFixture{id: "provider", facts: []InboundTransportCapabilityV2{bad}})
	if _, err = registry.Facts(context.Background(), now); err == nil {
		t.Fatal("contradictory provider fact accepted")
	}
}

func TestInboundTransportFactContainsNoExtensibleOrSecretFields(t *testing.T) {
	now := time.Unix(1000, 0)
	fact := validTransportFact(now, "core:inbound:1")
	if err := fact.Validate(now); err != nil {
		t.Fatal(err)
	}
	if fact.AuthenticationCount != 0 || fact.InboundTag != "" {
		t.Fatalf("unexpected identity data %#v", fact)
	}
}
