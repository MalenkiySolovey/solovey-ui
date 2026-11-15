package health

import (
	context "context"
	errors "errors"
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	sort "sort"
	strings "strings"
	sync "sync"
	atomic "sync/atomic"
)

const LocalProxyProbeCapabilitySchemaV1 = canonical.LocalProxyProbeCapabilitySchemaV1
const LocalProxyProbeObservationSchemaV1 = canonical.LocalProxyProbeObservationSchemaV1
const MaxLocalProxyProbeDurationV1 = canonical.MaxLocalProxyProbeDurationV1
const MaxLocalProxyProbeFreshnessV1 = canonical.MaxLocalProxyProbeFreshnessV1
const MaxLocalProxyProbeProvidersV1 = canonical.MaxLocalProxyProbeProvidersV1

type LocalProxyProbeTargetV1 = canonical.LocalProxyProbeTargetV1
type LocalProxyProbeCapabilityV1 = canonical.LocalProxyProbeCapabilityV1
type LocalProxyProbeRequestV1 = canonical.LocalProxyProbeRequestV1
type LocalProxyProbeObservationV1 = canonical.LocalProxyProbeObservationV1
type LocalProxyProbeProviderV1 = canonical.LocalProxyProbeProviderV1
type LocalProxyProbeRegistryV1 struct {
	mu             sync.Mutex
	providers      map[string]localProxyProbeProviderEntry
	lastGeneration map[string]uint64
	lastProbeID    map[string]string
	nonce          atomic.Uint64
}

type localProxyProbeProviderEntry struct {
	id       string
	instance string
	provider LocalProxyProbeProviderV1
}

func NewLocalProxyProbeRegistryV1() *LocalProxyProbeRegistryV1 {
	return &LocalProxyProbeRegistryV1{
		providers: map[string]localProxyProbeProviderEntry{}, lastGeneration: map[string]uint64{}, lastProbeID: map[string]string{},
	}
}

func (r *LocalProxyProbeRegistryV1) Register(provider LocalProxyProbeProviderV1) (func(), error) {
	id, instance, ok := localProxyProbeProviderIdentity(provider)
	if r == nil || !ok {
		return nil, errors.New("local_proxy_probe_provider_invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[id]; exists {
		return nil, errors.New("local_proxy_probe_provider_duplicate")
	}
	if len(r.providers) >= MaxLocalProxyProbeProvidersV1 {
		return nil, errors.New("local_proxy_probe_provider_capacity_exceeded")
	}
	r.providers[id] = localProxyProbeProviderEntry{id: id, instance: instance, provider: provider}
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if current, exists := r.providers[id]; exists && current.instance == instance {
				delete(r.providers, id)
				for key := range r.lastGeneration {
					if strings.HasPrefix(key, instance+"|") {
						delete(r.lastGeneration, key)
						delete(r.lastProbeID, key)
					}
				}
			}
		})
	}, nil
}

func (r *LocalProxyProbeRegistryV1) Capability(ctx context.Context, target LocalProxyProbeTargetV1) LocalProxyProbeCapabilityV1 {
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, MaxLocalProxyProbeDurationV1)
	defer cancel()
	providers := r.snapshotProviders()
	var selected LocalProxyProbeCapabilityV1
	count := 0
	for _, entry := range providers {
		value, ok := callLocalProxyProbeCapability(callCtx, entry.provider, target)
		if ok && value.ProviderID == entry.id && value.ProviderInstance == entry.instance && value.Available {
			selected, count = value, count+1
		}
	}
	if count != 1 || !validLocalProxyProbeCapability(selected, target) {
		return LocalProxyProbeCapabilityV1{
			Schema: LocalProxyProbeCapabilitySchemaV1, ResourceID: target.ResourceID, EndpointID: target.EndpointID,
			Protocol: target.Protocol, Available: false, ReasonCodes: []string{"LOCAL_PROXY_PROBE_UNAVAILABLE"},
		}
	}
	return selected
}

func (r *LocalProxyProbeRegistryV1) ProbeFresh(ctx context.Context, request LocalProxyProbeRequestV1) (LocalProxyProbeObservationV1, error) {
	if !validLocalProxyProbeRequest(request) {
		return LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_request_invalid")
	}
	capability := r.Capability(ctx, request.Target)
	if !capability.Available || capability.ProviderInstance != request.ProviderInstance {
		return LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_unavailable")
	}
	key := capability.ProviderInstance + "|" + request.Target.ResourceID + "|" + request.Target.EndpointID + "|" + string(request.Target.Protocol)
	r.mu.Lock()
	entry, exists := r.providers[capability.ProviderID]
	if !exists || entry.instance != capability.ProviderInstance {
		r.mu.Unlock()
		return LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_unavailable")
	}
	minimum := r.lastGeneration[key] + 1
	if request.MinimumGeneration > minimum {
		minimum = request.MinimumGeneration
	}
	r.mu.Unlock()
	request.MinimumGeneration = minimum
	request.ChallengeRevision = hostresources.Revision(struct {
		Schema, Instance, Target string
		Generation, Nonce        uint64
		Boundary                 int64
	}{
		LocalProxyProbeObservationSchemaV1, capability.ProviderInstance, hostresources.Revision(request.Target),
		minimum, r.nonce.Add(1), request.NotBeforeUnixNano,
	})
	probeCtx, cancel := context.WithTimeout(ctx, MaxLocalProxyProbeDurationV1)
	defer cancel()
	value, err, completed := callLocalProxyProbe(probeCtx, entry.provider, request)
	if !completed {
		return LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_timeout")
	}
	if err != nil {
		return LocalProxyProbeObservationV1{}, err
	}
	if err := validateLocalProxyProbeObservation(value, request, capability); err != nil {
		return LocalProxyProbeObservationV1{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if value.Generation <= r.lastGeneration[key] || value.ProbeID == r.lastProbeID[key] {
		return LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_replayed")
	}
	r.lastGeneration[key], r.lastProbeID[key] = value.Generation, value.ProbeID
	return value, nil
}

func (r *LocalProxyProbeRegistryV1) snapshotProviders() []localProxyProbeProviderEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]localProxyProbeProviderEntry, 0, len(r.providers))
	for _, provider := range r.providers {
		result = append(result, provider)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result
}

func localProxyProbeProviderIdentity(provider LocalProxyProbeProviderV1) (id, instance string, ok bool) {
	if provider == nil {
		return "", "", false
	}
	defer func() {
		if recover() != nil {
			id, instance, ok = "", "", false
		}
	}()
	id, instance = strings.TrimSpace(provider.ProviderID()), strings.TrimSpace(provider.ProviderInstance())
	return id, instance, healthToken(id, 128) && healthToken(instance, 128)
}

func callLocalProxyProbeCapability(ctx context.Context, provider LocalProxyProbeProviderV1, target LocalProxyProbeTargetV1) (LocalProxyProbeCapabilityV1, bool) {
	result := make(chan LocalProxyProbeCapabilityV1, 1)
	go func() {
		defer func() { _ = recover() }()
		result <- provider.Capability(ctx, target)
	}()
	select {
	case value := <-result:
		return value, true
	case <-ctx.Done():
		return LocalProxyProbeCapabilityV1{}, false
	}
}

func callLocalProxyProbe(ctx context.Context, provider LocalProxyProbeProviderV1, request LocalProxyProbeRequestV1) (LocalProxyProbeObservationV1, error, bool) {
	type response struct {
		value LocalProxyProbeObservationV1
		err   error
	}
	result := make(chan response, 1)
	go func() {
		defer func() {
			if recover() != nil {
				result <- response{err: errors.New("local_proxy_probe_failed")}
			}
		}()
		value, err := provider.Probe(ctx, request)
		result <- response{value: value, err: err}
	}()
	select {
	case value := <-result:
		return value.value, value.err, true
	case <-ctx.Done():
		return LocalProxyProbeObservationV1{}, ctx.Err(), false
	}
}

func FinalizeLocalProxyProbeCapabilityV1(value LocalProxyProbeCapabilityV1) LocalProxyProbeCapabilityV1 {
	return canonical.FinalizeLocalProxyProbeCapabilityV1(value)
}

func FinalizeLocalProxyProbeObservationV1(value LocalProxyProbeObservationV1) LocalProxyProbeObservationV1 {
	return canonical.FinalizeLocalProxyProbeObservationV1(value)
}

func validLocalProxyProbeCapability(value LocalProxyProbeCapabilityV1, target LocalProxyProbeTargetV1) bool {
	return canonical.LocalProxyProbeCapabilityValidV1(value, target)
}

func validLocalProxyProbeRequest(value LocalProxyProbeRequestV1) bool {
	return canonical.LocalProxyProbeRequestValidV1(value)
}

func validateLocalProxyProbeObservation(value LocalProxyProbeObservationV1, request LocalProxyProbeRequestV1, capability LocalProxyProbeCapabilityV1) error {
	return canonical.ValidateLocalProxyProbeObservationV1(value, request, capability)
}

var DefaultLocalProxyProbesV1 = NewLocalProxyProbeRegistryV1()
