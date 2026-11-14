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

const ProtocolProbeCapabilitySchemaV1 = canonical.ProtocolProbeCapabilitySchemaV1
const ProtocolProbeObservationSchemaV1 = canonical.ProtocolProbeObservationSchemaV1
const MaxProtocolProbeDuration = canonical.MaxProtocolProbeDuration
const MaxProtocolProbeFreshness = canonical.MaxProtocolProbeFreshness
const MaxProtocolProbeProvidersV1 = canonical.MaxProtocolProbeProvidersV1

type ProtocolProbeTargetV1 = canonical.ProtocolProbeTargetV1
type ProtocolProbeCapabilityV1 = canonical.ProtocolProbeCapabilityV1
type ProtocolProbeRequestV1 = canonical.ProtocolProbeRequestV1
type ProtocolProbeObservationV1 = canonical.ProtocolProbeObservationV1
type ProtocolProbeProviderV1 = canonical.ProtocolProbeProviderV1
type ProtocolProbeRegistryV1 struct {
	mu             sync.Mutex
	providers      map[string]protocolProbeProviderEntry
	lastGeneration map[string]uint64
	lastProbeID    map[string]string
	nonce          atomic.Uint64
}

type protocolProbeProviderEntry struct {
	id       string
	instance string
	provider ProtocolProbeProviderV1
}

func NewProtocolProbeRegistryV1() *ProtocolProbeRegistryV1 {
	return &ProtocolProbeRegistryV1{providers: map[string]protocolProbeProviderEntry{}, lastGeneration: map[string]uint64{}, lastProbeID: map[string]string{}}
}

func (r *ProtocolProbeRegistryV1) Register(provider ProtocolProbeProviderV1) (func(), error) {
	id, instance, ok := protocolProbeProviderIdentity(provider)
	if r == nil || !ok {
		return nil, errors.New("protocol_probe_provider_invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[id]; exists {
		return nil, errors.New("protocol_probe_provider_duplicate")
	}
	if len(r.providers) >= MaxProtocolProbeProvidersV1 {
		return nil, errors.New("protocol_probe_provider_capacity_exceeded")
	}
	r.providers[id] = protocolProbeProviderEntry{id: id, instance: instance, provider: provider}
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			if current, exists := r.providers[id]; exists && current.instance == instance {
				delete(r.providers, id)
				prefix := instance + "|"
				for key := range r.lastGeneration {
					if strings.HasPrefix(key, prefix) {
						delete(r.lastGeneration, key)
						delete(r.lastProbeID, key)
					}
				}
			}
			r.mu.Unlock()
		})
	}, nil
}

func (r *ProtocolProbeRegistryV1) Capability(ctx context.Context, target ProtocolProbeTargetV1) ProtocolProbeCapabilityV1 {
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, MaxProtocolProbeDuration)
	defer cancel()
	providers := r.snapshotProviders()
	var selected ProtocolProbeCapabilityV1
	count := 0
	for _, entry := range providers {
		value, ok := callProtocolProbeCapability(callCtx, entry.provider, target)
		if ok && value.ProviderID == entry.id && value.ProviderInstance == entry.instance && value.Available {
			selected = value
			count++
		}
	}
	if count != 1 || !validProtocolCapability(selected, target) {
		return ProtocolProbeCapabilityV1{Schema: ProtocolProbeCapabilitySchemaV1, ResourceID: target.ResourceID, EndpointID: target.EndpointID, ProtocolClass: target.ProtocolClass, Available: false, ReasonCodes: []string{"PROTOCOL_PROBE_UNAVAILABLE"}}
	}
	return selected
}

func (r *ProtocolProbeRegistryV1) ProbeFresh(ctx context.Context, request ProtocolProbeRequestV1) (ProtocolProbeObservationV1, error) {
	if !validProtocolProbeRequest(request) {
		return ProtocolProbeObservationV1{}, errors.New("protocol_probe_request_invalid")
	}
	capability := r.Capability(ctx, request.Target)
	if !capability.Available || capability.ProviderInstance != request.ProviderInstance {
		return ProtocolProbeObservationV1{}, errors.New("protocol_probe_unavailable")
	}
	r.mu.Lock()
	entry, exists := r.providers[capability.ProviderID]
	if !exists || entry.instance != capability.ProviderInstance {
		r.mu.Unlock()
		return ProtocolProbeObservationV1{}, errors.New("protocol_probe_unavailable")
	}
	key := capability.ProviderInstance + "|" + request.Target.ResourceID + "|" + request.Target.EndpointID
	minimum := r.lastGeneration[key] + 1
	if request.MinimumGeneration > minimum {
		minimum = request.MinimumGeneration
	}
	r.mu.Unlock()
	request.MinimumGeneration = minimum
	request.ChallengeRevision = hostresources.Revision(struct {
		Schema, Instance, Resource, Endpoint string
		Generation, Nonce                    uint64
		Boundary                             int64
	}{
		ProtocolProbeObservationSchemaV1, capability.ProviderInstance, request.Target.ResourceID, request.Target.EndpointID, minimum, r.nonce.Add(1), request.NotBeforeUnixNano})
	probeCtx, cancel := context.WithTimeout(ctx, MaxProtocolProbeDuration)
	defer cancel()
	value, err, completed := callProtocolProbe(probeCtx, entry.provider, request)
	if !completed {
		return ProtocolProbeObservationV1{}, errors.New("protocol_probe_timeout")
	}
	if err != nil {
		return ProtocolProbeObservationV1{}, err
	}
	if err := validateProtocolObservation(value, request, capability); err != nil {
		return ProtocolProbeObservationV1{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if value.Generation <= r.lastGeneration[key] || value.ProbeID == r.lastProbeID[key] {
		return ProtocolProbeObservationV1{}, errors.New("protocol_probe_replayed")
	}
	r.lastGeneration[key], r.lastProbeID[key] = value.Generation, value.ProbeID
	return value, nil
}

func (r *ProtocolProbeRegistryV1) snapshotProviders() []protocolProbeProviderEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]protocolProbeProviderEntry, 0, len(r.providers))
	for _, p := range r.providers {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result
}

func protocolProbeProviderIdentity(provider ProtocolProbeProviderV1) (id, instance string, ok bool) {
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

func callProtocolProbeCapability(ctx context.Context, provider ProtocolProbeProviderV1, target ProtocolProbeTargetV1) (ProtocolProbeCapabilityV1, bool) {
	result := make(chan ProtocolProbeCapabilityV1, 1)
	go func() {
		defer func() { _ = recover() }()
		result <- provider.Capability(ctx, target)
	}()
	select {
	case value := <-result:
		return value, true
	case <-ctx.Done():
		return ProtocolProbeCapabilityV1{}, false
	}
}

func callProtocolProbe(ctx context.Context, provider ProtocolProbeProviderV1, request ProtocolProbeRequestV1) (ProtocolProbeObservationV1, error, bool) {
	type response struct {
		value ProtocolProbeObservationV1
		err   error
	}
	result := make(chan response, 1)
	go func() {
		defer func() {
			if recover() != nil {
				result <- response{err: errors.New("protocol_probe_failed")}
			}
		}()
		value, err := provider.Probe(ctx, request)
		result <- response{value: value, err: err}
	}()
	select {
	case value := <-result:
		return value.value, value.err, true
	case <-ctx.Done():
		return ProtocolProbeObservationV1{}, ctx.Err(), false
	}
}

func validProtocolCapability(value ProtocolProbeCapabilityV1, target ProtocolProbeTargetV1) bool {
	return canonical.ProtocolProbeCapabilityValidV1(value, target)
}

func FinalizeProtocolProbeCapabilityV1(value ProtocolProbeCapabilityV1) ProtocolProbeCapabilityV1 {
	return canonical.FinalizeProtocolProbeCapabilityV1(value)
}

func FinalizeProtocolProbeObservationV1(value ProtocolProbeObservationV1) ProtocolProbeObservationV1 {
	return canonical.FinalizeProtocolProbeObservationV1(value)
}

func validateProtocolObservation(value ProtocolProbeObservationV1, request ProtocolProbeRequestV1, capability ProtocolProbeCapabilityV1) error {
	return canonical.ValidateProtocolProbeObservationV1(value, request, capability)
}

func validProtocolProbeRequest(value ProtocolProbeRequestV1) bool {
	return canonical.ProtocolProbeRequestValidV1(value)
}

var DefaultProtocolProbesV1 = NewProtocolProbeRegistryV1()
