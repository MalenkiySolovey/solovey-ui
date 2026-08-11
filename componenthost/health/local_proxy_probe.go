package health

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const (
	LocalProxyProbeCapabilitySchemaV1  = "solovey-ui/local-proxy-probe-capability/v1"
	LocalProxyProbeObservationSchemaV1 = "solovey-ui/local-proxy-probe-observation/v1"
	MaxLocalProxyProbeDurationV1       = 5 * time.Second
	MaxLocalProxyProbeFreshnessV1      = time.Minute
)

type LocalProxyProbeTargetV1 struct {
	ProviderID                  string
	ResourceID                  string
	EndpointID                  string
	Protocol                    hostresources.LocalProxyProtocolV1
	ConfigurationRevision       string
	RuntimeRevision             string
	FactRevision                string
	ListenerObservationRevision string
	AuthenticationRevision      string
	TLSRevision                 string
	SystemProxyRevision         string
	LeaseID                     string
	LeaseRevision               string
	LeaseState                  hostresources.EndpointLeaseState
	OperationID                 string
	OperationRevision           int
	PlanRevision                string
	MarkerRevision              string
}

type LocalProxyProbeCapabilityV1 struct {
	Schema           string                             `json:"schema"`
	ProviderID       string                             `json:"providerId"`
	ProviderInstance string                             `json:"providerInstance"`
	ResourceID       string                             `json:"resourceId"`
	EndpointID       string                             `json:"endpointId"`
	Protocol         hostresources.LocalProxyProtocolV1 `json:"protocol"`
	Available        bool                               `json:"available"`
	Revision         string                             `json:"revision"`
	ReasonCodes      []string                           `json:"reasonCodes,omitempty"`
}

type LocalProxyProbeRequestV1 struct {
	Target            LocalProxyProbeTargetV1
	ProviderInstance  string
	MinimumGeneration uint64
	NotBeforeUnixNano int64
	ChallengeRevision string
}

type LocalProxyProbeObservationV1 struct {
	Schema                      string                             `json:"schema"`
	ProviderID                  string                             `json:"providerId"`
	ProviderInstance            string                             `json:"providerInstance"`
	ResourceID                  string                             `json:"resourceId"`
	EndpointID                  string                             `json:"endpointId"`
	Protocol                    hostresources.LocalProxyProtocolV1 `json:"protocol"`
	ConfigurationRevision       string                             `json:"configurationRevision"`
	RuntimeRevision             string                             `json:"runtimeRevision"`
	FactRevision                string                             `json:"factRevision"`
	ListenerObservationRevision string                             `json:"listenerObservationRevision"`
	AuthenticationRevision      string                             `json:"authenticationRevision"`
	TLSRevision                 string                             `json:"tlsRevision"`
	SystemProxyRevision         string                             `json:"systemProxyRevision"`
	LeaseID                     string                             `json:"leaseId"`
	LeaseRevision               string                             `json:"leaseRevision"`
	LeaseState                  hostresources.EndpointLeaseState   `json:"leaseState"`
	OperationID                 string                             `json:"operationId"`
	OperationRevision           int                                `json:"operationRevision"`
	PlanRevision                string                             `json:"planRevision"`
	MarkerRevision              string                             `json:"markerRevision"`
	ChallengeRevision           string                             `json:"challengeRevision"`
	Generation                  uint64                             `json:"generation"`
	ProbeID                     string                             `json:"probeId"`
	StartedUnixNano             int64                              `json:"startedUnixNano"`
	CompletedUnixNano           int64                              `json:"completedUnixNano"`
	ExpiresUnixNano             int64                              `json:"expiresUnixNano"`
	Passed                      bool                               `json:"passed"`
	PositiveTransaction         bool                               `json:"positiveTransaction"`
	MissingAuthenticationDenied bool                               `json:"missingAuthenticationDenied"`
	InvalidAuthenticationDenied bool                               `json:"invalidAuthenticationDenied"`
	ExactTarget                 bool                               `json:"exactTarget"`
	ExactSink                   bool                               `json:"exactSink"`
	SinkRevision                string                             `json:"sinkRevision"`
	ResponderRevision           string                             `json:"responderRevision"`
	Revision                    string                             `json:"revision"`
	ReasonCodes                 []string                           `json:"reasonCodes,omitempty"`
}

type LocalProxyProbeProviderV1 interface {
	ProviderID() string
	ProviderInstance() string
	Capability(context.Context, LocalProxyProbeTargetV1) LocalProxyProbeCapabilityV1
	Probe(context.Context, LocalProxyProbeRequestV1) (LocalProxyProbeObservationV1, error)
}

type LocalProxyProbeRegistryV1 struct {
	mu             sync.Mutex
	providers      map[string]LocalProxyProbeProviderV1
	lastGeneration map[string]uint64
	lastProbeID    map[string]string
	nonce          atomic.Uint64
}

func NewLocalProxyProbeRegistryV1() *LocalProxyProbeRegistryV1 {
	return &LocalProxyProbeRegistryV1{
		providers: map[string]LocalProxyProbeProviderV1{}, lastGeneration: map[string]uint64{}, lastProbeID: map[string]string{},
	}
}

func (r *LocalProxyProbeRegistryV1) Register(provider LocalProxyProbeProviderV1) (func(), error) {
	if r == nil || provider == nil || strings.TrimSpace(provider.ProviderID()) == "" || strings.TrimSpace(provider.ProviderInstance()) == "" {
		return nil, errors.New("local_proxy_probe_provider_invalid")
	}
	id, instance := provider.ProviderID(), provider.ProviderInstance()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[id]; exists {
		return nil, errors.New("local_proxy_probe_provider_duplicate")
	}
	r.providers[id] = provider
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if current := r.providers[id]; current != nil && current.ProviderInstance() == instance {
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
	providers := r.snapshotProviders()
	var selected LocalProxyProbeCapabilityV1
	count := 0
	for _, provider := range providers {
		value := provider.Capability(ctx, target)
		if value.Available {
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
	provider := r.providers[capability.ProviderID]
	if provider == nil || provider.ProviderInstance() != capability.ProviderInstance {
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
	value, err := provider.Probe(probeCtx, request)
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

func (r *LocalProxyProbeRegistryV1) snapshotProviders() []LocalProxyProbeProviderV1 {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]LocalProxyProbeProviderV1, 0, len(r.providers))
	for _, provider := range r.providers {
		result = append(result, provider)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ProviderID() < result[j].ProviderID() })
	return result
}

func FinalizeLocalProxyProbeCapabilityV1(value LocalProxyProbeCapabilityV1) LocalProxyProbeCapabilityV1 {
	value.Schema = LocalProxyProbeCapabilitySchemaV1
	value.ReasonCodes = sortedCodes(value.ReasonCodes)
	value.Revision = hostresources.Revision(localProxyProbeCapabilityRevisionInput(value))
	return value
}

func FinalizeLocalProxyProbeObservationV1(value LocalProxyProbeObservationV1) LocalProxyProbeObservationV1 {
	value.Schema = LocalProxyProbeObservationSchemaV1
	value.ReasonCodes = sortedCodes(value.ReasonCodes)
	value.Revision = hostresources.Revision(localProxyProbeObservationRevisionInput(value))
	return value
}

func validLocalProxyProbeCapability(value LocalProxyProbeCapabilityV1, target LocalProxyProbeTargetV1) bool {
	return value.Schema == LocalProxyProbeCapabilitySchemaV1 && value.ProviderID != "" && value.ProviderInstance != "" &&
		value.ResourceID == target.ResourceID && value.EndpointID == target.EndpointID && value.Protocol == target.Protocol &&
		value.Available && value.Revision == hostresources.Revision(localProxyProbeCapabilityRevisionInput(value))
}

func validLocalProxyProbeRequest(value LocalProxyProbeRequestV1) bool {
	target := value.Target
	return validLocalProxyProbeProtocol(target.Protocol) && target.ProviderID != "" && target.ResourceID != "" &&
		target.EndpointID != "" && validProbeRevision(target.ConfigurationRevision) && validProbeRevision(target.RuntimeRevision) &&
		validProbeRevision(target.FactRevision) && validProbeRevision(target.ListenerObservationRevision) &&
		validProbeRevision(target.AuthenticationRevision) && validProbeRevision(target.TLSRevision) &&
		validProbeRevision(target.SystemProxyRevision) && target.LeaseID != "" && validProbeRevision(target.LeaseRevision) &&
		(target.LeaseState == hostresources.EndpointLeaseMutationPending || target.LeaseState == hostresources.EndpointLeaseActive) &&
		target.OperationID != "" && target.OperationRevision > 0 && validProbeRevision(target.PlanRevision) &&
		validProbeRevision(target.MarkerRevision) && value.ProviderInstance != "" && value.NotBeforeUnixNano > 0
}

func validateLocalProxyProbeObservation(value LocalProxyProbeObservationV1, request LocalProxyProbeRequestV1, capability LocalProxyProbeCapabilityV1) error {
	now := time.Now().UTC().UnixNano()
	target := request.Target
	if value.Schema != LocalProxyProbeObservationSchemaV1 || value.ProviderID != capability.ProviderID ||
		value.ProviderInstance != capability.ProviderInstance || value.ResourceID != target.ResourceID ||
		value.EndpointID != target.EndpointID || value.Protocol != target.Protocol ||
		value.ConfigurationRevision != target.ConfigurationRevision || value.RuntimeRevision != target.RuntimeRevision ||
		value.FactRevision != target.FactRevision || value.ListenerObservationRevision != target.ListenerObservationRevision ||
		value.AuthenticationRevision != target.AuthenticationRevision || value.TLSRevision != target.TLSRevision ||
		value.SystemProxyRevision != target.SystemProxyRevision || value.LeaseID != target.LeaseID ||
		value.LeaseRevision != target.LeaseRevision || value.LeaseState != target.LeaseState ||
		value.OperationID != target.OperationID || value.OperationRevision != target.OperationRevision ||
		value.PlanRevision != target.PlanRevision || value.MarkerRevision != target.MarkerRevision ||
		value.ChallengeRevision != request.ChallengeRevision || value.Generation < request.MinimumGeneration ||
		value.ProbeID == "" || value.StartedUnixNano < request.NotBeforeUnixNano ||
		value.CompletedUnixNano < value.StartedUnixNano || value.CompletedUnixNano > now ||
		value.CompletedUnixNano-value.StartedUnixNano > MaxLocalProxyProbeDurationV1.Nanoseconds() ||
		value.ExpiresUnixNano <= now || value.ExpiresUnixNano-value.CompletedUnixNano > MaxLocalProxyProbeFreshnessV1.Nanoseconds() ||
		!value.Passed || !value.PositiveTransaction || !value.ExactTarget || !value.ExactSink ||
		!validProbeRevision(value.SinkRevision) ||
		value.ResponderRevision != hostresources.Revision(struct{ Challenge, Probe, Sink string }{
			value.ChallengeRevision, value.ProbeID, value.SinkRevision,
		}) || len(value.ReasonCodes) != 0 ||
		value.Revision != hostresources.Revision(localProxyProbeObservationRevisionInput(value)) {
		return errors.New("local_proxy_probe_observation_invalid")
	}
	return nil
}

func localProxyProbeCapabilityRevisionInput(value LocalProxyProbeCapabilityV1) LocalProxyProbeCapabilityV1 {
	value.Revision = ""
	return value
}

func localProxyProbeObservationRevisionInput(value LocalProxyProbeObservationV1) LocalProxyProbeObservationV1 {
	value.ChallengeRevision, value.ProbeID, value.SinkRevision, value.ResponderRevision, value.Revision = "", "", "", "", ""
	value.Generation, value.StartedUnixNano, value.CompletedUnixNano, value.ExpiresUnixNano = 0, 0, 0, 0
	return value
}

func validLocalProxyProbeProtocol(value hostresources.LocalProxyProtocolV1) bool {
	switch value {
	case hostresources.LocalProxyProtocolSOCKS4, hostresources.LocalProxyProtocolSOCKS5,
		hostresources.LocalProxyProtocolHTTPForward, hostresources.LocalProxyProtocolHTTPConnect:
		return true
	default:
		return false
	}
}

var DefaultLocalProxyProbesV1 = NewLocalProxyProbeRegistryV1()
