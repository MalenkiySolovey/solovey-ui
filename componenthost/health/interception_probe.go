package health

import (
	context "context"
	errors "errors"
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/health"
	sort "sort"
	strings "strings"
	sync "sync"
	time "time"
)

const InterceptionProbeTargetSchemaV1 = canonical.InterceptionProbeTargetSchemaV1
const InterceptionProbeObservationSchemaV1 = canonical.InterceptionProbeObservationSchemaV1
const MaxInterceptionProbeProvidersV1 = canonical.MaxInterceptionProbeProvidersV1

type InterceptionProbeTargetV1 = canonical.InterceptionProbeTargetV1
type InterceptionProbeRequestV1 = canonical.InterceptionProbeRequestV1
type InterceptionProbeObservationV1 = canonical.InterceptionProbeObservationV1
type InterceptionProbeProviderV1 = canonical.InterceptionProbeProviderV1
type InterceptionProbeRegistryV1 struct {
	mu        sync.RWMutex
	next      uint64
	providers map[uint64]interceptionProbeProviderEntry
}

type interceptionProbeProviderEntry struct {
	id       string
	provider InterceptionProbeProviderV1
}

func NewInterceptionProbeRegistryV1() *InterceptionProbeRegistryV1 {
	return &InterceptionProbeRegistryV1{providers: map[uint64]interceptionProbeProviderEntry{}}
}

func (r *InterceptionProbeRegistryV1) Register(provider InterceptionProbeProviderV1) (func(), error) {
	providerID, ok := interceptionProbeProviderID(provider)
	if r == nil || !ok {
		return func() {}, errors.New("interception_probe_provider_v1_invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.providers) >= MaxInterceptionProbeProvidersV1 {
		return func() {}, errors.New("interception_probe_provider_v1_capacity_exceeded")
	}
	for _, current := range r.providers {
		if current.id == providerID {
			return func() {}, errors.New("interception_probe_provider_v1_duplicate")
		}
	}
	id := r.next
	r.next++
	r.providers[id] = interceptionProbeProviderEntry{id: providerID, provider: provider}
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			delete(r.providers, id)
			r.mu.Unlock()
		})
	}, nil
}

func (r *InterceptionProbeRegistryV1) ProviderCount() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.providers)
}

func (r *InterceptionProbeRegistryV1) Probe(ctx context.Context, request InterceptionProbeRequestV1) (InterceptionProbeObservationV1, error) {
	if request.Validate() != nil {
		return InterceptionProbeObservationV1{}, errors.New("interception_probe_request_v1_invalid")
	}
	timeout := time.Until(time.Unix(request.DeadlineAt, 0))
	if timeout <= 0 || timeout > 30*time.Second {
		return InterceptionProbeObservationV1{}, errors.New("interception_probe_request_v1_deadline_invalid")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	providers := r.snapshot()
	selected := make([]interceptionProbeProviderEntry, 0, 1)
	for _, entry := range providers {
		if callSupportsInterceptionProbe(callCtx, entry.provider, request.Target) {
			selected = append(selected, entry)
		}
	}
	if len(selected) != 1 {
		return InterceptionProbeObservationV1{}, errors.New("interception_probe_provider_v1_missing_or_ambiguous")
	}
	observation, err, completed := callInterceptionProbe(callCtx, selected[0].provider, request)
	if !completed {
		return InterceptionProbeObservationV1{}, errors.New("interception_probe_provider_v1_timeout")
	}
	if err != nil || observation.ProviderID != selected[0].id || observation.Validate(request) != nil {
		return InterceptionProbeObservationV1{}, errors.New("interception_probe_observation_v1_invalid")
	}
	return observation, nil
}

func (r *InterceptionProbeRegistryV1) snapshot() []interceptionProbeProviderEntry {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]interceptionProbeProviderEntry, 0, len(r.providers))
	for _, provider := range r.providers {
		result = append(result, provider)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result
}

func interceptionProbeProviderID(provider InterceptionProbeProviderV1) (id string, ok bool) {
	if provider == nil {
		return "", false
	}
	defer func() {
		if recover() != nil {
			id, ok = "", false
		}
	}()
	id = strings.TrimSpace(provider.ProviderID())
	return id, healthToken(id, 128)
}

func callSupportsInterceptionProbe(ctx context.Context, provider InterceptionProbeProviderV1, target InterceptionProbeTargetV1) bool {
	result := make(chan bool, 1)
	go func() {
		defer func() { _ = recover() }()
		result <- provider.SupportsInterceptionProbeV1(target)
	}()
	select {
	case supported := <-result:
		return supported
	case <-ctx.Done():
		return false
	}
}

func callInterceptionProbe(ctx context.Context, provider InterceptionProbeProviderV1, request InterceptionProbeRequestV1) (InterceptionProbeObservationV1, error, bool) {
	type response struct {
		value InterceptionProbeObservationV1
		err   error
	}
	result := make(chan response, 1)
	go func() {
		defer func() {
			if recover() != nil {
				result <- response{err: errors.New("interception_probe_provider_v1_failed")}
			}
		}()
		value, err := provider.ProbeInterceptionV1(ctx, request)
		result <- response{value: value, err: err}
	}()
	select {
	case value := <-result:
		return value.value, value.err, true
	case <-ctx.Done():
		return InterceptionProbeObservationV1{}, ctx.Err(), false
	}
}

func healthToken(value string, limit int) bool {
	return canonical.HealthContractTokenValidV1(value, limit)
}

var DefaultInterceptionProbesV1 = NewInterceptionProbeRegistryV1()
