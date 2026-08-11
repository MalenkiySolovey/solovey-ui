package resources

import (
	"context"
	"strconv"
	"testing"
	"time"
)

type inboundTransportProviderFixture struct {
	id string
}

func (provider inboundTransportProviderFixture) ProviderID() string {
	return provider.id
}

func (inboundTransportProviderFixture) InboundTransportCapabilitiesV2(context.Context, time.Time) ([]InboundTransportCapabilityV2, error) {
	return nil, nil
}

func TestInboundTransportRegistryRejectsDuplicateAuthority(t *testing.T) {
	registry := NewInboundTransportRegistryV2()
	unregister, err := registry.Register(inboundTransportProviderFixture{id: "core"})
	if err != nil {
		t.Fatalf("register first provider: %v", err)
	}
	if _, err := registry.Register(inboundTransportProviderFixture{id: "core"}); err == nil {
		t.Fatal("registry accepted a second provider with the same authority id")
	}
	unregister()
	if _, err := registry.Register(inboundTransportProviderFixture{id: "core"}); err != nil {
		t.Fatalf("provider id remained reserved after unregister: %v", err)
	}
}

func TestInboundTransportRegistryBoundsProviderCardinality(t *testing.T) {
	registry := NewInboundTransportRegistryV2()
	for index := 0; index < MaxInboundTransportProvidersV2; index++ {
		id := "provider-" + strconv.Itoa(index)
		if _, err := registry.Register(inboundTransportProviderFixture{id: id}); err != nil {
			t.Fatalf("register provider %d: %v", index, err)
		}
	}
	if _, err := registry.Register(inboundTransportProviderFixture{id: "overflow"}); err == nil {
		t.Fatal("registry accepted an unbounded provider")
	}
}
