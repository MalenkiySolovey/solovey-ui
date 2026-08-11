package resources

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

const MaxFrontingBackendProvidersV1 = 128

// FrontingBackendProviderV1 is the ordinary endpoint owner's combined fact
// and lease authority. A consumer cannot register a fact-only mirror as an
// actionable backend.
type FrontingBackendProviderV1 interface {
	EndpointLeaseProviderV1
	FrontingBackendFactsV1(context.Context, time.Time) ([]FrontingBackendFactV1, error)
}

type FrontingBackendRegistryV1 struct {
	mu        sync.RWMutex
	next      uint64
	providers map[uint64]FrontingBackendProviderV1
}

func NewFrontingBackendRegistryV1() *FrontingBackendRegistryV1 {
	return &FrontingBackendRegistryV1{providers: make(map[uint64]FrontingBackendProviderV1)}
}

func (r *FrontingBackendRegistryV1) Register(provider FrontingBackendProviderV1) (func(), error) {
	if r == nil || provider == nil || !frontingToken(provider.ProviderID(), 128) {
		return func() {}, errors.New("fronting_backend_provider_v1_invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.providers) >= MaxFrontingBackendProvidersV1 {
		return func() {}, errors.New("fronting_backend_provider_v1_capacity_exceeded")
	}
	for _, current := range r.providers {
		if current.ProviderID() == provider.ProviderID() {
			return func() {}, errors.New("fronting_backend_provider_v1_duplicate")
		}
	}
	id := r.next
	r.next++
	r.providers[id] = provider
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); delete(r.providers, id); r.mu.Unlock() }) }, nil
}

func (r *FrontingBackendRegistryV1) EndpointLeaseProviderV1(providerID string) (EndpointLeaseProviderV1, bool) {
	if r == nil || !frontingToken(providerID, 128) {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, provider := range r.providers {
		if provider.ProviderID() == providerID {
			return provider, true
		}
	}
	return nil, false
}

func (r *FrontingBackendRegistryV1) FactsV1(ctx context.Context, now time.Time) ([]FrontingBackendFactV1, error) {
	if r == nil {
		return nil, errors.New("fronting_backend_registry_v1_unavailable")
	}
	r.mu.RLock()
	providers := make([]FrontingBackendProviderV1, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	r.mu.RUnlock()
	sort.Slice(providers, func(i, j int) bool { return providers[i].ProviderID() < providers[j].ProviderID() })
	if len(providers) == 0 {
		return nil, errors.New("fronting_backend_provider_v1_absent")
	}
	result := make([]FrontingBackendFactV1, 0)
	seen := make(map[string]struct{})
	for _, provider := range providers {
		callCtx, cancel := context.WithTimeout(ctx, DefaultTimeoutForFrontingProviderV1)
		facts, err := provider.FrontingBackendFactsV1(callCtx, now)
		cancel()
		if err != nil {
			return nil, errors.New("fronting_backend_provider_v1_unavailable")
		}
		for _, fact := range facts {
			key := fact.ProviderID + "\x00" + fact.ResourceID + "\x00" + fact.EndpointID
			if fact.ProviderID != provider.ProviderID() || fact.Validate() != nil {
				return nil, errors.New("fronting_backend_fact_v1_invalid")
			}
			if _, duplicate := seen[key]; duplicate {
				return nil, errors.New("fronting_backend_fact_v1_ambiguous")
			}
			seen[key] = struct{}{}
			result = append(result, fact)
			if len(result) > MaxResourceFacts {
				return nil, errors.New("fronting_backend_fact_v1_truncated")
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left := result[i].ProviderID + "\x00" + result[i].ResourceID + "\x00" + result[i].EndpointID
		right := result[j].ProviderID + "\x00" + result[j].ResourceID + "\x00" + result[j].EndpointID
		return left < right
	})
	return result, nil
}

func (r *FrontingBackendRegistryV1) ResolveV1(ctx context.Context, reference FrontingBackendReferenceV1, now time.Time) (FrontingBackendFactV1, error) {
	facts, err := r.FactsV1(ctx, now)
	if err != nil {
		return FrontingBackendFactV1{}, err
	}
	for _, fact := range facts {
		if fact.ProviderID == reference.ProviderID && fact.ResourceID == reference.ResourceID && fact.EndpointID == reference.EndpointID {
			if err := ResolveExactFrontingBackendV1(reference, fact, now); err != nil {
				return FrontingBackendFactV1{}, err
			}
			return fact, nil
		}
	}
	return FrontingBackendFactV1{}, errors.New("fronting_backend_reference_v1_missing")
}

func (r *FrontingBackendRegistryV1) EndpointLeasesByHolderV1(ctx context.Context, holderID string) ([]EndpointLeaseV1, error) {
	if !frontingToken(holderID, 128) {
		return nil, errors.New("endpoint_lease_holder_v1_invalid")
	}
	r.mu.RLock()
	providers := make([]FrontingBackendProviderV1, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	r.mu.RUnlock()
	result := make([]EndpointLeaseV1, 0)
	for _, provider := range providers {
		callCtx, cancel := context.WithTimeout(ctx, DefaultTimeoutForFrontingProviderV1)
		leases, err := provider.ListEndpointLeases(callCtx, ListEndpointLeasesRequestV1{HolderID: holderID, Limit: MaxEndpointLeasePageV1})
		cancel()
		if err != nil {
			return nil, err
		}
		result = append(result, leases...)
		if len(result) > MaxEndpointLeasePageV1 {
			return nil, errors.New("endpoint_lease_list_v1_truncated")
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LeaseID < result[j].LeaseID })
	return result, nil
}

const DefaultTimeoutForFrontingProviderV1 = 3 * time.Second

var DefaultFrontingBackendsV1 = NewFrontingBackendRegistryV1()

func RegisterFrontingBackendProviderV1(provider FrontingBackendProviderV1) (func(), error) {
	return DefaultFrontingBackendsV1.Register(provider)
}
