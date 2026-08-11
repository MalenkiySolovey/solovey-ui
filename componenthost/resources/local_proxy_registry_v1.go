package resources

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

const MaxLocalProxyProvidersV1 = 128

type LocalProxyRegistryV1 struct {
	mu        sync.RWMutex
	next      uint64
	providers map[uint64]LocalProxyProviderV1
}

func NewLocalProxyRegistryV1() *LocalProxyRegistryV1 {
	return &LocalProxyRegistryV1{providers: make(map[uint64]LocalProxyProviderV1)}
}

func (r *LocalProxyRegistryV1) Register(provider LocalProxyProviderV1) (func(), error) {
	if r == nil || provider == nil || !frontingToken(provider.ProviderID(), 128) {
		return func() {}, errors.New("local_proxy_provider_v1_invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.providers) >= MaxLocalProxyProvidersV1 {
		return func() {}, errors.New("local_proxy_provider_v1_capacity_exceeded")
	}
	for _, current := range r.providers {
		if current.ProviderID() == provider.ProviderID() {
			return func() {}, errors.New("local_proxy_provider_v1_duplicate")
		}
	}
	id := r.next
	r.next++
	r.providers[id] = provider
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); delete(r.providers, id); r.mu.Unlock() }) }, nil
}

func (r *LocalProxyRegistryV1) Provider(providerID string) (LocalProxyProviderV1, bool) {
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

func (r *LocalProxyRegistryV1) FactsV1(ctx context.Context, now time.Time) ([]LocalProxyFactV1, error) {
	if r == nil {
		return nil, errors.New("local_proxy_registry_v1_unavailable")
	}
	providers := r.snapshotProviders()
	if len(providers) == 0 {
		return nil, errors.New("local_proxy_provider_v1_absent")
	}
	result := make([]LocalProxyFactV1, 0)
	seen := map[string]bool{}
	for _, provider := range providers {
		callCtx, cancel := context.WithTimeout(ctx, DefaultTimeoutForFrontingProviderV1)
		facts, err := provider.LocalProxyFactsV1(callCtx, now)
		cancel()
		if err != nil {
			return nil, errors.New("local_proxy_provider_v1_unavailable")
		}
		for _, fact := range facts {
			key := fact.ProviderID + "\x00" + fact.ResourceID + "\x00" + fact.EndpointID
			if fact.ProviderID != provider.ProviderID() || fact.Validate(now) != nil || seen[key] {
				return nil, errors.New("local_proxy_fact_v1_invalid_or_ambiguous")
			}
			seen[key] = true
			result = append(result, fact)
			if len(result) > MaxResourceFacts {
				return nil, errors.New("local_proxy_fact_v1_truncated")
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ProviderID+"\x00"+result[i].ResourceID+"\x00"+result[i].EndpointID <
			result[j].ProviderID+"\x00"+result[j].ResourceID+"\x00"+result[j].EndpointID
	})
	return result, nil
}

func (r *LocalProxyRegistryV1) ResolveV1(ctx context.Context, reference LocalProxyReferenceV1, now time.Time) (LocalProxyFactV1, error) {
	facts, err := r.FactsV1(ctx, now)
	if err != nil {
		return LocalProxyFactV1{}, err
	}
	for _, fact := range facts {
		if fact.ProviderID == reference.ProviderID && fact.ResourceID == reference.ResourceID && fact.EndpointID == reference.EndpointID {
			if err := ResolveExactLocalProxyV1(reference, fact, now); err != nil {
				return LocalProxyFactV1{}, err
			}
			return fact, nil
		}
	}
	return LocalProxyFactV1{}, errors.New("local_proxy_reference_v1_missing")
}

func (r *LocalProxyRegistryV1) LeasesByHolderV1(ctx context.Context, holderID string) ([]LocalProxyGuardLeaseV1, error) {
	if !frontingToken(holderID, 128) {
		return nil, errors.New("local_proxy_guard_lease_holder_v1_invalid")
	}
	result := make([]LocalProxyGuardLeaseV1, 0)
	for _, provider := range r.snapshotProviders() {
		callCtx, cancel := context.WithTimeout(ctx, DefaultTimeoutForFrontingProviderV1)
		leases, err := provider.ListLocalProxyGuardLeases(callCtx, ListLocalProxyGuardLeasesRequestV1{HolderID: holderID, Limit: MaxLocalProxyGuardLeasePageV1})
		cancel()
		if err != nil {
			return nil, err
		}
		result = append(result, leases...)
		if len(result) > MaxLocalProxyGuardLeasePageV1 {
			return nil, errors.New("local_proxy_guard_lease_list_v1_truncated")
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LeaseID < result[j].LeaseID })
	return result, nil
}

func (r *LocalProxyRegistryV1) snapshotProviders() []LocalProxyProviderV1 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]LocalProxyProviderV1, 0, len(r.providers))
	for _, provider := range r.providers {
		result = append(result, provider)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ProviderID() < result[j].ProviderID() })
	return result
}

var DefaultLocalProxiesV1 = NewLocalProxyRegistryV1()

func RegisterLocalProxyProviderV1(provider LocalProxyProviderV1) (func(), error) {
	return DefaultLocalProxiesV1.Register(provider)
}
