package resources

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

const MaxInterceptionProvidersV1 = 128

type InterceptionRegistryV1 struct {
	mu        sync.RWMutex
	next      uint64
	providers map[uint64]InterceptionProviderV1
}

func NewInterceptionRegistryV1() *InterceptionRegistryV1 {
	return &InterceptionRegistryV1{providers: make(map[uint64]InterceptionProviderV1)}
}

func (r *InterceptionRegistryV1) Register(provider InterceptionProviderV1) (func(), error) {
	if r == nil || provider == nil || !interceptionToken(provider.ProviderID(), 128) {
		return func() {}, errors.New("interception_provider_v1_invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.providers) >= MaxInterceptionProvidersV1 {
		return func() {}, errors.New("interception_provider_v1_capacity_exceeded")
	}
	for _, current := range r.providers {
		if current.ProviderID() == provider.ProviderID() {
			return func() {}, errors.New("interception_provider_v1_duplicate")
		}
	}
	id := r.next
	r.next++
	r.providers[id] = provider
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); delete(r.providers, id); r.mu.Unlock() }) }, nil
}

func (r *InterceptionRegistryV1) Provider(providerID string) (InterceptionProviderV1, bool) {
	for _, provider := range r.snapshotProviders() {
		if provider.ProviderID() == providerID {
			return provider, true
		}
	}
	return nil, false
}

func (r *InterceptionRegistryV1) FactsV1(ctx context.Context, now time.Time) ([]InterceptionInboundFactV1, error) {
	providers := r.snapshotProviders()
	if len(providers) == 0 {
		return nil, errors.New("interception_provider_v1_absent")
	}
	result := make([]InterceptionInboundFactV1, 0)
	seen := map[string]bool{}
	for _, provider := range providers {
		callCtx, cancel := context.WithTimeout(ctx, DefaultTimeoutForFrontingProviderV1)
		facts, err := provider.InterceptionFactsV1(callCtx, now)
		cancel()
		if err != nil {
			return nil, errors.New("interception_provider_v1_unavailable")
		}
		for _, fact := range facts {
			key := fact.ProviderID + "\x00" + fact.ResourceID + "\x00" + fact.EndpointID
			if fact.ProviderID != provider.ProviderID() || fact.Validate(now) != nil || seen[key] {
				return nil, errors.New("interception_fact_v1_invalid_or_ambiguous")
			}
			seen[key] = true
			result = append(result, fact)
			if len(result) > MaxResourceFacts {
				return nil, errors.New("interception_fact_v1_truncated")
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ProviderID+"\x00"+result[i].ResourceID+"\x00"+result[i].EndpointID <
			result[j].ProviderID+"\x00"+result[j].ResourceID+"\x00"+result[j].EndpointID
	})
	return result, nil
}

func (r *InterceptionRegistryV1) ResolveV1(ctx context.Context, reference InterceptionReferenceV1, now time.Time) (InterceptionInboundFactV1, error) {
	facts, err := r.FactsV1(ctx, now)
	if err != nil {
		return InterceptionInboundFactV1{}, err
	}
	for _, fact := range facts {
		if fact.ProviderID == reference.ProviderID && fact.ResourceID == reference.ResourceID && fact.EndpointID == reference.EndpointID &&
			fact.Network == reference.Network && fact.AddressFamily == reference.AddressFamily {
			if err := ResolveExactInterceptionV1(reference, fact, now); err != nil {
				return InterceptionInboundFactV1{}, err
			}
			return fact, nil
		}
	}
	return InterceptionInboundFactV1{}, errors.New("interception_reference_v1_missing")
}

func (r *InterceptionRegistryV1) snapshotProviders() []InterceptionProviderV1 {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]InterceptionProviderV1, 0, len(r.providers))
	for _, provider := range r.providers {
		result = append(result, provider)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ProviderID() < result[j].ProviderID() })
	return result
}

type ForwardedIngressScopeRegistryV1 struct {
	mu        sync.RWMutex
	next      uint64
	providers map[uint64]ForwardedIngressScopeProviderV1
}

func NewForwardedIngressScopeRegistryV1() *ForwardedIngressScopeRegistryV1 {
	return &ForwardedIngressScopeRegistryV1{providers: make(map[uint64]ForwardedIngressScopeProviderV1)}
}

func (r *ForwardedIngressScopeRegistryV1) Register(provider ForwardedIngressScopeProviderV1) (func(), error) {
	if r == nil || provider == nil || !interceptionToken(provider.ProviderID(), 128) {
		return func() {}, errors.New("forwarded_ingress_provider_v1_invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.providers) >= MaxInterceptionProvidersV1 {
		return func() {}, errors.New("forwarded_ingress_provider_v1_capacity_exceeded")
	}
	for _, current := range r.providers {
		if current.ProviderID() == provider.ProviderID() {
			return func() {}, errors.New("forwarded_ingress_provider_v1_duplicate")
		}
	}
	id := r.next
	r.next++
	r.providers[id] = provider
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); delete(r.providers, id); r.mu.Unlock() }) }, nil
}

func (r *ForwardedIngressScopeRegistryV1) FactsV1(ctx context.Context, now time.Time) ([]ForwardedIngressScopeFactV1, error) {
	if r == nil {
		return nil, errors.New("forwarded_ingress_registry_v1_unavailable")
	}
	r.mu.RLock()
	providers := make([]ForwardedIngressScopeProviderV1, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	r.mu.RUnlock()
	if len(providers) == 0 {
		return nil, errors.New("forwarded_ingress_provider_v1_absent")
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].ProviderID() < providers[j].ProviderID() })
	result := make([]ForwardedIngressScopeFactV1, 0)
	seen := map[string]bool{}
	for _, provider := range providers {
		callCtx, cancel := context.WithTimeout(ctx, DefaultTimeoutForFrontingProviderV1)
		facts, err := provider.ForwardedIngressScopesV1(callCtx, now)
		cancel()
		if err != nil {
			return nil, errors.New("forwarded_ingress_provider_v1_unavailable")
		}
		for _, fact := range facts {
			key := fact.ProviderID + "\x00" + fact.ScopeID + "\x00" + string(fact.AddressFamily)
			if fact.ProviderID != provider.ProviderID() || fact.Validate(now) != nil || seen[key] {
				return nil, errors.New("forwarded_ingress_scope_fact_v1_invalid_or_ambiguous")
			}
			seen[key] = true
			result = append(result, fact)
			if len(result) > MaxResourceFacts {
				return nil, errors.New("forwarded_ingress_scope_fact_v1_truncated")
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ProviderID+"\x00"+result[i].ScopeID+"\x00"+string(result[i].AddressFamily) <
			result[j].ProviderID+"\x00"+result[j].ScopeID+"\x00"+string(result[j].AddressFamily)
	})
	return result, nil
}

func (r *ForwardedIngressScopeRegistryV1) ResolveV1(ctx context.Context, reference ForwardedIngressScopeReferenceV1, now time.Time) (ForwardedIngressScopeFactV1, error) {
	facts, err := r.FactsV1(ctx, now)
	if err != nil {
		return ForwardedIngressScopeFactV1{}, err
	}
	for _, fact := range facts {
		if fact.ProviderID == reference.ProviderID && fact.ScopeID == reference.ScopeID && fact.AddressFamily == reference.AddressFamily {
			if err := ResolveExactIngressScopeV1(reference, fact, now); err != nil {
				return ForwardedIngressScopeFactV1{}, err
			}
			return fact, nil
		}
	}
	return ForwardedIngressScopeFactV1{}, errors.New("forwarded_ingress_scope_reference_v1_missing")
}

var (
	DefaultInterceptionsV1 = NewInterceptionRegistryV1()
	DefaultIngressScopesV1 = NewForwardedIngressScopeRegistryV1()
)

func RegisterInterceptionProviderV1(provider InterceptionProviderV1) (func(), error) {
	return DefaultInterceptionsV1.Register(provider)
}
func RegisterForwardedIngressScopeProviderV1(provider ForwardedIngressScopeProviderV1) (func(), error) {
	return DefaultIngressScopesV1.Register(provider)
}
