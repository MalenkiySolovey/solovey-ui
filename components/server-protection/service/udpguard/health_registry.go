package udpguard

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

type HealthProvider interface {
	ProviderID() string
	UDPStrategyHealth(context.Context, time.Time) ([]UDPStrategyHealthV1, error)
}

type HealthRegistry struct {
	mu        sync.RWMutex
	next      uint64
	providers map[uint64]HealthProvider
}

const maxHealthProviders = 128

func NewHealthRegistry() *HealthRegistry {
	return &HealthRegistry{providers: make(map[uint64]HealthProvider)}
}

func (r *HealthRegistry) Register(provider HealthProvider) (func(), error) {
	if r == nil || provider == nil || provider.ProviderID() == "" || len(provider.ProviderID()) > 128 {
		return nil, errors.New("udp_health_provider_invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.providers) >= maxHealthProviders {
		return nil, errors.New("udp_health_provider_capacity_exceeded")
	}
	for _, current := range r.providers {
		if current.ProviderID() == provider.ProviderID() {
			return nil, errors.New("udp_health_provider_ambiguous")
		}
	}
	r.next++
	id := r.next
	r.providers[id] = provider
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); delete(r.providers, id); r.mu.Unlock() }) }, nil
}

func (r *HealthRegistry) Snapshot(ctx context.Context, now time.Time) (map[string]UDPStrategyHealthV1, error) {
	r.mu.RLock()
	providers := make([]HealthProvider, 0, len(r.providers))
	for _, p := range r.providers {
		providers = append(providers, p)
	}
	r.mu.RUnlock()
	sort.Slice(providers, func(i, j int) bool { return providers[i].ProviderID() < providers[j].ProviderID() })
	result := map[string]UDPStrategyHealthV1{}
	for _, provider := range providers {
		values, err := provider.UDPStrategyHealth(ctx, now)
		if err != nil {
			return nil, err
		}
		for _, value := range values {
			key := value.ResourceID + "|" + value.EndpointID
			if len(result) >= MaxHealthFactsV1 {
				return nil, errors.New("udp_health_fact_cardinality_exceeded")
			}
			if _, ok := result[key]; ok || value.Validate(time.Time{}) != nil {
				return nil, errors.New("udp_health_fact_invalid")
			}
			result[key] = value
		}
	}
	return result, nil
}

var DefaultHealth = NewHealthRegistry()
