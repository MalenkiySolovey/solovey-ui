package fronting

import (
	"context"
	"errors"
	"sort"
	"sync"
)

// ExactHealthProbeV2 is the production traffic-evidence boundary. Implementors
// own the public fixture and backend identity markers; the fronting component
// only validates their exact, revision-bound evidence.
type ExactHealthProbeV2 interface {
	ProviderID() string
	ProbeFixedL4V2(context.Context, FixedL4HealthRequestV2) (FixedL4HealthEvidenceV2, error)
	ProbeSNIPrereadV2(context.Context, SNIPrereadHealthRequestV2) (SNIPrereadHealthEvidenceV2, error)
}

type ExactHealthRegistryV2 struct {
	mu        sync.RWMutex
	next      uint64
	providers map[uint64]ExactHealthProbeV2
}

const maxExactHealthProvidersV2 = 64

func NewExactHealthRegistryV2() *ExactHealthRegistryV2 {
	return &ExactHealthRegistryV2{providers: make(map[uint64]ExactHealthProbeV2)}
}

func (r *ExactHealthRegistryV2) Register(provider ExactHealthProbeV2) (func(), error) {
	if r == nil || provider == nil || safeRuntimeTokenV2(provider.ProviderID(), 128) == "" {
		return func() {}, errors.New("fronting_health_provider_invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.providers) >= maxExactHealthProvidersV2 {
		return func() {}, errors.New("fronting_health_provider_capacity_exceeded")
	}
	for _, current := range r.providers {
		if current.ProviderID() == provider.ProviderID() {
			return func() {}, errors.New("fronting_health_provider_ambiguous")
		}
	}
	id := r.next
	r.next++
	r.providers[id] = provider
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); delete(r.providers, id); r.mu.Unlock() }) }, nil
}

func (r *ExactHealthRegistryV2) provider() (ExactHealthProbeV2, error) {
	if r == nil {
		return nil, errors.New("fronting_health_provider_unavailable")
	}
	r.mu.RLock()
	providers := make([]ExactHealthProbeV2, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	r.mu.RUnlock()
	sort.Slice(providers, func(i, j int) bool { return providers[i].ProviderID() < providers[j].ProviderID() })
	if len(providers) != 1 {
		return nil, errors.New("fronting_health_provider_unavailable")
	}
	return providers[0], nil
}

func (r *ExactHealthRegistryV2) FixedL4Check() FixedL4HealthCheckV2 {
	return func(ctx context.Context, request FixedL4HealthRequestV2) (FixedL4HealthEvidenceV2, error) {
		provider, err := r.provider()
		if err != nil {
			return FixedL4HealthEvidenceV2{}, err
		}
		return provider.ProbeFixedL4V2(ctx, request)
	}
}

func (r *ExactHealthRegistryV2) SNIPrereadCheck() SNIPrereadHealthCheckV2 {
	return func(ctx context.Context, request SNIPrereadHealthRequestV2) (SNIPrereadHealthEvidenceV2, error) {
		provider, err := r.provider()
		if err != nil {
			return SNIPrereadHealthEvidenceV2{}, err
		}
		return provider.ProbeSNIPrereadV2(ctx, request)
	}
}

var DefaultExactHealthRegistryV2 = NewExactHealthRegistryV2()
