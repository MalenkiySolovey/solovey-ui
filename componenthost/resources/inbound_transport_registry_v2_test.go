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

type mutableInboundTransportProviderFixture struct {
	id       string
	blocking bool
	panic    bool
}

func (provider *mutableInboundTransportProviderFixture) ProviderID() string { return provider.id }
func (provider *mutableInboundTransportProviderFixture) InboundTransportCapabilitiesV2(context.Context, time.Time) ([]InboundTransportCapabilityV2, error) {
	if provider.panic {
		panic("secret")
	}
	if provider.blocking {
		time.Sleep(250 * time.Millisecond)
	}
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

func TestInboundTransportRegistryPinsIdentityAndContainsProviderFailure(t *testing.T) {
	registry := NewInboundTransportRegistryV2()
	provider := &mutableInboundTransportProviderFixture{id: "stable"}
	cleanup, err := registry.Register(provider)
	if err != nil {
		t.Fatal(err)
	}
	provider.id = "changed"
	if _, err := registry.Register(inboundTransportProviderFixture{id: "stable"}); err == nil {
		t.Fatal("mutable provider identity released its registered authority")
	}
	cleanup()

	provider = &mutableInboundTransportProviderFixture{id: "blocking", blocking: true}
	if _, err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := registry.Facts(ctx, time.Now()); err == nil {
		t.Fatal("blocking provider was treated as available")
	}
	if time.Since(started) > 200*time.Millisecond {
		t.Fatal("provider call escaped the registry deadline")
	}

	panicRegistry := NewInboundTransportRegistryV2()
	if _, err := panicRegistry.Register(&mutableInboundTransportProviderFixture{id: "panic", panic: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := panicRegistry.Facts(context.Background(), time.Now()); err == nil {
		t.Fatal("panicking provider was treated as available")
	}
}
