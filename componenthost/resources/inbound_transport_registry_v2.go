package resources

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

const MaxInboundTransportProvidersV2 = 128

type InboundTransportCapabilityProviderV2 interface {
	ProviderID() string
	InboundTransportCapabilitiesV2(context.Context, time.Time) ([]InboundTransportCapabilityV2, error)
}

type InboundTransportRegistryV2 struct {
	mu        sync.RWMutex
	next      uint64
	providers map[uint64]identifiedProvider[InboundTransportCapabilityProviderV2]
}

func NewInboundTransportRegistryV2() *InboundTransportRegistryV2 {
	return &InboundTransportRegistryV2{providers: make(map[uint64]identifiedProvider[InboundTransportCapabilityProviderV2])}
}

func (r *InboundTransportRegistryV2) Register(provider InboundTransportCapabilityProviderV2) (func(), error) {
	providerID, ok := stableProviderID(provider, boundedToken)
	if r == nil || !ok {
		return nil, errors.New("inbound_transport_provider_v2_invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.providers) >= MaxInboundTransportProvidersV2 {
		return nil, errors.New("inbound_transport_provider_v2_capacity_exceeded")
	}
	for _, current := range r.providers {
		if current.id == providerID {
			return nil, errors.New("inbound_transport_provider_v2_duplicate")
		}
	}
	r.next++
	id := r.next
	r.providers[id] = identifiedProvider[InboundTransportCapabilityProviderV2]{id: providerID, provider: provider}
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); delete(r.providers, id); r.mu.Unlock() }) }, nil
}

func (r *InboundTransportRegistryV2) Facts(ctx context.Context, now time.Time) ([]InboundTransportCapabilityV2, error) {
	if r == nil {
		return nil, errors.New("inbound_transport_registry_v2_unavailable")
	}
	r.mu.RLock()
	providers := make([]identifiedProvider[InboundTransportCapabilityProviderV2], 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	r.mu.RUnlock()
	sort.Slice(providers, func(i, j int) bool { return providers[i].id < providers[j].id })
	result := make([]InboundTransportCapabilityV2, 0)
	seen := map[string]bool{}
	for _, entry := range providers {
		facts, err := callResourceProvider(ctx, DefaultTimeoutForFrontingProviderV1, func(callCtx context.Context) ([]InboundTransportCapabilityV2, error) {
			return entry.provider.InboundTransportCapabilitiesV2(callCtx, now)
		})
		if err != nil {
			return nil, errors.New("inbound_transport_provider_v2_unavailable")
		}
		for _, fact := range facts {
			if fact.ProviderID != entry.id || seen[fact.ResourceID] || fact.Validate(now) != nil {
				return nil, errors.New("inbound_transport_provider_v2_fact_invalid")
			}
			seen[fact.ResourceID] = true
			result = append(result, fact)
			if len(result) > MaxInboundTransportFactsV2 {
				return nil, errors.New("inbound_transport_provider_v2_cardinality_exceeded")
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ResourceID < result[j].ResourceID })
	return result, nil
}

var DefaultInboundTransportsV2 = NewInboundTransportRegistryV2()

func RegisterInboundTransportCapabilityProviderV2(provider InboundTransportCapabilityProviderV2) (func(), error) {
	return DefaultInboundTransportsV2.Register(provider)
}
