package health

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const (
	InterceptionProbeTargetSchemaV1      = "solovey-ui/interception-probe-target/v1"
	InterceptionProbeObservationSchemaV1 = "solovey-ui/interception-probe-observation/v1"
	MaxInterceptionProbeProvidersV1      = 32
)

type InterceptionProbeTargetV1 struct {
	Schema                      string `json:"schema"`
	ProviderID                  string `json:"providerId"`
	ResourceID                  string `json:"resourceId"`
	EndpointID                  string `json:"endpointId"`
	Kind                        string `json:"kind"`
	Network                     string `json:"network"`
	AddressFamily               string `json:"addressFamily"`
	IngressScopeProviderID      string `json:"ingressScopeProviderId"`
	IngressScopeID              string `json:"ingressScopeId"`
	IngressScopeRevision        string `json:"ingressScopeRevision"`
	InterceptionFactRevision    string `json:"interceptionFactRevision"`
	RuntimeRevision             string `json:"runtimeRevision"`
	ListenerRevision            string `json:"listenerRevision"`
	ManagedCandidateRevision    string `json:"managedCandidateRevision"`
	PolicyRoutingRevision       string `json:"policyRoutingRevision,omitempty"`
	ManagementExclusionRevision string `json:"managementExclusionRevision"`
	HealthTransactionRevision   string `json:"healthTransactionRevision"`
}

type InterceptionProbeRequestV1 struct {
	Target            InterceptionProbeTargetV1 `json:"target"`
	OperationID       string                    `json:"operationId"`
	MarkerRevision    string                    `json:"markerRevision"`
	RuntimeGeneration string                    `json:"runtimeGeneration"`
	Challenge         string                    `json:"challenge"`
	MarkerAt          int64                     `json:"markerAt"`
	DeadlineAt        int64                     `json:"deadlineAt"`
}

type InterceptionProbeObservationV1 struct {
	Schema                       string `json:"schema"`
	ProviderID                   string `json:"providerId"`
	TargetRevision               string `json:"targetRevision"`
	OperationID                  string `json:"operationId"`
	MarkerRevision               string `json:"markerRevision"`
	RuntimeGeneration            string `json:"runtimeGeneration"`
	Challenge                    string `json:"challenge"`
	StartedAt                    int64  `json:"startedAt"`
	CompletedAt                  int64  `json:"completedAt"`
	RequestSent                  bool   `json:"requestSent"`
	ResponseReceived             bool   `json:"responseReceived"`
	IntendedInboundObserved      bool   `json:"intendedInboundObserved"`
	IntendedSinkObserved         bool   `json:"intendedSinkObserved"`
	WrongSinkUntouched           bool   `json:"wrongSinkUntouched"`
	OriginalDestinationPreserved bool   `json:"originalDestinationPreserved"`
	SourcePreserved              bool   `json:"sourcePreserved"`
	ManagementPreserved          bool   `json:"managementPreserved"`
	BoundedFlowExpired           bool   `json:"boundedFlowExpired"`
}

type InterceptionProbeProviderV1 interface {
	ProviderID() string
	SupportsInterceptionProbeV1(InterceptionProbeTargetV1) bool
	ProbeInterceptionV1(context.Context, InterceptionProbeRequestV1) (InterceptionProbeObservationV1, error)
}

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

func (t InterceptionProbeTargetV1) Validate() error {
	if t.Schema != InterceptionProbeTargetSchemaV1 ||
		!healthToken(t.ProviderID, 128) || !healthToken(t.ResourceID, 256) || !healthToken(t.EndpointID, 256) ||
		(t.Kind != "REDIRECT" && t.Kind != "TPROXY") || (t.Network != "tcp" && t.Network != "udp") ||
		(t.AddressFamily != "ipv4" && t.AddressFamily != "ipv6") ||
		!healthToken(t.IngressScopeProviderID, 128) || !healthToken(t.IngressScopeID, 256) ||
		!healthDigest(t.IngressScopeRevision) || !healthDigest(t.InterceptionFactRevision) ||
		!healthDigest(t.RuntimeRevision) || !healthDigest(t.ListenerRevision) ||
		!healthDigest(t.ManagedCandidateRevision) || !healthDigest(t.ManagementExclusionRevision) ||
		!healthDigest(t.HealthTransactionRevision) ||
		t.Kind == "REDIRECT" && (t.Network != "tcp" || t.PolicyRoutingRevision != "") ||
		t.Kind == "TPROXY" && !healthDigest(t.PolicyRoutingRevision) {
		return errors.New("interception_probe_target_v1_invalid")
	}
	return nil
}

func (t InterceptionProbeTargetV1) Revision() string {
	return hostresources.Revision(t)
}

func (r InterceptionProbeRequestV1) Validate() error {
	if r.Target.Validate() != nil || !healthToken(r.OperationID, 128) ||
		!healthDigest(r.MarkerRevision) || !healthToken(r.RuntimeGeneration, 128) ||
		!healthToken(r.Challenge, 128) || r.MarkerAt <= 0 || r.DeadlineAt <= r.MarkerAt ||
		r.DeadlineAt-r.MarkerAt > 30 {
		return errors.New("interception_probe_request_v1_invalid")
	}
	return nil
}

func (o InterceptionProbeObservationV1) Validate(request InterceptionProbeRequestV1) error {
	if request.Validate() != nil || o.Schema != InterceptionProbeObservationSchemaV1 ||
		!healthToken(o.ProviderID, 128) || o.TargetRevision != request.Target.Revision() ||
		o.OperationID != request.OperationID || o.MarkerRevision != request.MarkerRevision ||
		o.RuntimeGeneration != request.RuntimeGeneration || o.Challenge != request.Challenge ||
		o.StartedAt <= request.MarkerAt || o.CompletedAt < o.StartedAt || o.CompletedAt > request.DeadlineAt ||
		!o.RequestSent || !o.ResponseReceived || !o.IntendedInboundObserved || !o.IntendedSinkObserved ||
		!o.WrongSinkUntouched || !o.OriginalDestinationPreserved || !o.SourcePreserved || !o.ManagementPreserved ||
		request.Target.Network == "udp" && !o.BoundedFlowExpired {
		return errors.New("interception_probe_observation_v1_invalid")
	}
	return nil
}

func healthToken(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("._:@+-", r) {
			continue
		}
		return false
	}
	return true
}

func healthDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' && r < 'a' || r > 'f' {
			return false
		}
	}
	return true
}

var DefaultInterceptionProbesV1 = NewInterceptionProbeRegistryV1()
