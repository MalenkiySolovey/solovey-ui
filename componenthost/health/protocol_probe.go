package health

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const (
	ProtocolProbeCapabilitySchemaV1  = "solovey-ui/protocol-probe-capability/v1"
	ProtocolProbeObservationSchemaV1 = "solovey-ui/protocol-probe-observation/v1"
	MaxProtocolProbeDuration         = 5 * time.Second
	MaxProtocolProbeFreshness        = time.Minute
)

type ProtocolProbeTargetV1 struct {
	ResourceID            string
	EndpointID            string
	ProtocolClass         hostresources.InboundTransportClass
	RuntimeRevision       string
	CapabilityRevision    string
	ConfigurationRevision string
	SocketRevision        string
	AddressFamily         hostresources.AddressFamily
	ConfiguredBind        string
	Port                  uint16
}

type ProtocolProbeCapabilityV1 struct {
	Schema           string                              `json:"schema"`
	ProviderID       string                              `json:"providerId"`
	ProviderInstance string                              `json:"providerInstance"`
	ResourceID       string                              `json:"resourceId"`
	EndpointID       string                              `json:"endpointId"`
	ProtocolClass    hostresources.InboundTransportClass `json:"protocolClass"`
	Available        bool                                `json:"available"`
	Revision         string                              `json:"revision"`
	ReasonCodes      []string                            `json:"reasonCodes,omitempty"`
}

type ProtocolProbeRequestV1 struct {
	Target               ProtocolProbeTargetV1
	ContributionRevision string
	CompositionRevision  string
	ManagedPlanRevision  string
	ProviderInstance     string
	MinimumGeneration    uint64
	NotBeforeUnixNano    int64
	ChallengeRevision    string
}

type ProtocolProbeObservationV1 struct {
	Schema                string                              `json:"schema"`
	ProviderID            string                              `json:"providerId"`
	ProviderInstance      string                              `json:"providerInstance"`
	ResourceID            string                              `json:"resourceId"`
	EndpointID            string                              `json:"endpointId"`
	ProtocolClass         hostresources.InboundTransportClass `json:"protocolClass"`
	RuntimeRevision       string                              `json:"runtimeRevision"`
	CapabilityRevision    string                              `json:"capabilityRevision"`
	ConfigurationRevision string                              `json:"configurationRevision"`
	SocketRevision        string                              `json:"socketRevision"`
	ContributionRevision  string                              `json:"contributionRevision"`
	CompositionRevision   string                              `json:"compositionRevision"`
	ManagedPlanRevision   string                              `json:"managedPlanRevision"`
	ChallengeRevision     string                              `json:"challengeRevision"`
	Generation            uint64                              `json:"generation"`
	ProbeID               string                              `json:"probeId"`
	StartedUnixNano       int64                               `json:"startedUnixNano"`
	CompletedUnixNano     int64                               `json:"completedUnixNano"`
	ExpiresUnixNano       int64                               `json:"expiresUnixNano"`
	Passed                bool                                `json:"passed"`
	RequestResponse       bool                                `json:"requestResponse"`
	ExactTarget           bool                                `json:"exactTarget"`
	ResponderRevision     string                              `json:"responderRevision"`
	Revision              string                              `json:"revision"`
	ReasonCodes           []string                            `json:"reasonCodes,omitempty"`
}

type ProtocolProbeProviderV1 interface {
	ProviderID() string
	ProviderInstance() string
	Capability(context.Context, ProtocolProbeTargetV1) ProtocolProbeCapabilityV1
	Probe(context.Context, ProtocolProbeRequestV1) (ProtocolProbeObservationV1, error)
}

type ProtocolProbeRegistryV1 struct {
	mu             sync.Mutex
	providers      map[string]ProtocolProbeProviderV1
	lastGeneration map[string]uint64
	lastProbeID    map[string]string
	nonce          atomic.Uint64
}

func NewProtocolProbeRegistryV1() *ProtocolProbeRegistryV1 {
	return &ProtocolProbeRegistryV1{providers: map[string]ProtocolProbeProviderV1{}, lastGeneration: map[string]uint64{}, lastProbeID: map[string]string{}}
}

func (r *ProtocolProbeRegistryV1) Register(provider ProtocolProbeProviderV1) (func(), error) {
	if r == nil || provider == nil || strings.TrimSpace(provider.ProviderID()) == "" || strings.TrimSpace(provider.ProviderInstance()) == "" {
		return nil, errors.New("protocol_probe_provider_invalid")
	}
	id := provider.ProviderID()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[id]; exists {
		return nil, errors.New("protocol_probe_provider_duplicate")
	}
	r.providers[id] = provider
	var once sync.Once
	instance := provider.ProviderInstance()
	return func() {
		once.Do(func() {
			r.mu.Lock()
			if current := r.providers[id]; current != nil && current.ProviderInstance() == instance {
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
	providers := r.snapshotProviders()
	var selected ProtocolProbeCapabilityV1
	count := 0
	for _, provider := range providers {
		value := provider.Capability(ctx, target)
		if value.Available {
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
	provider := r.providers[capability.ProviderID]
	if provider == nil || provider.ProviderInstance() != capability.ProviderInstance {
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
	value, err := provider.Probe(probeCtx, request)
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

func (r *ProtocolProbeRegistryV1) snapshotProviders() []ProtocolProbeProviderV1 {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]ProtocolProbeProviderV1, 0, len(r.providers))
	for _, p := range r.providers {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ProviderID() < result[j].ProviderID() })
	return result
}

func validProtocolCapability(value ProtocolProbeCapabilityV1, target ProtocolProbeTargetV1) bool {
	return value.Schema == ProtocolProbeCapabilitySchemaV1 && value.ProviderID != "" && value.ProviderInstance != "" && value.ResourceID == target.ResourceID && value.EndpointID == target.EndpointID && value.ProtocolClass == target.ProtocolClass && value.Available && value.Revision == hostresources.Revision(protocolCapabilityRevisionInput(value))
}
func FinalizeProtocolProbeCapabilityV1(value ProtocolProbeCapabilityV1) ProtocolProbeCapabilityV1 {
	value.Schema = ProtocolProbeCapabilitySchemaV1
	value.ReasonCodes = sortedCodes(value.ReasonCodes)
	value.Revision = hostresources.Revision(protocolCapabilityRevisionInput(value))
	return value
}
func protocolCapabilityRevisionInput(value ProtocolProbeCapabilityV1) ProtocolProbeCapabilityV1 {
	value.Revision = ""
	return value
}

func FinalizeProtocolProbeObservationV1(value ProtocolProbeObservationV1) ProtocolProbeObservationV1 {
	value.Schema = ProtocolProbeObservationSchemaV1
	value.ReasonCodes = sortedCodes(value.ReasonCodes)
	value.Revision = hostresources.Revision(protocolObservationRevisionInput(value))
	return value
}
func protocolObservationRevisionInput(value ProtocolProbeObservationV1) ProtocolProbeObservationV1 {
	value.ChallengeRevision = ""
	value.Generation = 0
	value.ProbeID = ""
	value.StartedUnixNano = 0
	value.CompletedUnixNano = 0
	value.ExpiresUnixNano = 0
	value.ResponderRevision = ""
	value.Revision = ""
	return value
}

func validateProtocolObservation(value ProtocolProbeObservationV1, request ProtocolProbeRequestV1, capability ProtocolProbeCapabilityV1) error {
	now := time.Now().UTC().UnixNano()
	if value.Schema != ProtocolProbeObservationSchemaV1 || value.ProviderID != capability.ProviderID || value.ProviderInstance != capability.ProviderInstance ||
		value.ResourceID != request.Target.ResourceID || value.EndpointID != request.Target.EndpointID || value.ProtocolClass != request.Target.ProtocolClass ||
		value.RuntimeRevision != request.Target.RuntimeRevision || value.CapabilityRevision != request.Target.CapabilityRevision || value.ConfigurationRevision != request.Target.ConfigurationRevision || value.SocketRevision != request.Target.SocketRevision ||
		value.ContributionRevision != request.ContributionRevision || value.CompositionRevision != request.CompositionRevision || value.ManagedPlanRevision != request.ManagedPlanRevision ||
		value.ChallengeRevision != request.ChallengeRevision || value.Generation < request.MinimumGeneration || value.ProbeID == "" || value.StartedUnixNano < request.NotBeforeUnixNano ||
		value.CompletedUnixNano < value.StartedUnixNano || value.CompletedUnixNano > now || value.CompletedUnixNano-value.StartedUnixNano > MaxProtocolProbeDuration.Nanoseconds() ||
		value.ExpiresUnixNano <= now || value.ExpiresUnixNano-value.CompletedUnixNano > MaxProtocolProbeFreshness.Nanoseconds() ||
		!value.Passed || !value.RequestResponse || !value.ExactTarget || value.ResponderRevision != hostresources.Revision(struct{ Challenge, Probe string }{value.ChallengeRevision, value.ProbeID}) ||
		len(value.ReasonCodes) != 0 || value.Revision != hostresources.Revision(protocolObservationRevisionInput(value)) {
		return errors.New("protocol_probe_observation_invalid")
	}
	return nil
}

func validProtocolProbeRequest(value ProtocolProbeRequestV1) bool {
	address, err := netip.ParseAddr(strings.TrimSpace(value.Target.ConfiguredBind))
	if err != nil || value.Target.ResourceID == "" || value.Target.EndpointID == "" || value.Target.ProtocolClass == "" || value.Target.Port == 0 || value.ProviderInstance == "" || value.NotBeforeUnixNano <= 0 ||
		!validProbeRevision(value.Target.RuntimeRevision) || !validProbeRevision(value.Target.CapabilityRevision) || !validProbeRevision(value.Target.ConfigurationRevision) || !validProbeRevision(value.Target.SocketRevision) ||
		!validProbeRevision(value.ContributionRevision) || !validProbeRevision(value.CompositionRevision) || !validProbeRevision(value.ManagedPlanRevision) {
		return false
	}
	return value.Target.AddressFamily == hostresources.AddressFamilyIPv4 && address.Is4() || value.Target.AddressFamily == hostresources.AddressFamilyIPv6 && address.Is6()
}

func validProbeRevision(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

func sortedCodes(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

var DefaultProtocolProbesV1 = NewProtocolProbeRegistryV1()
