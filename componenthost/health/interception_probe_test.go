package health

import (
	"context"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

type interceptionProbeFixture struct {
	id     string
	mutate func(*InterceptionProbeObservationV1)
}

func (p interceptionProbeFixture) ProviderID() string { return p.id }
func (interceptionProbeFixture) SupportsInterceptionProbeV1(InterceptionProbeTargetV1) bool {
	return true
}
func (p interceptionProbeFixture) ProbeInterceptionV1(_ context.Context, request InterceptionProbeRequestV1) (InterceptionProbeObservationV1, error) {
	value := InterceptionProbeObservationV1{
		Schema: InterceptionProbeObservationSchemaV1, ProviderID: p.id,
		TargetRevision: request.Target.Revision(), OperationID: request.OperationID,
		MarkerRevision: request.MarkerRevision, RuntimeGeneration: request.RuntimeGeneration,
		Challenge: request.Challenge, StartedAt: request.MarkerAt + 1, CompletedAt: request.MarkerAt + 2,
		RequestSent: true, ResponseReceived: true, IntendedInboundObserved: true,
		IntendedSinkObserved: true, WrongSinkUntouched: true,
		OriginalDestinationPreserved: true, SourcePreserved: true,
		ManagementPreserved: true, BoundedFlowExpired: request.Target.Network == "udp",
	}
	if p.mutate != nil {
		p.mutate(&value)
	}
	return value, nil
}

func interceptionProbeRequestFixture() InterceptionProbeRequestV1 {
	digest := hostresources.Revision("fixture")
	return InterceptionProbeRequestV1{
		Target: InterceptionProbeTargetV1{
			Schema: InterceptionProbeTargetSchemaV1, ProviderID: "core", ResourceID: "inbound:1",
			EndpointID: "endpoint:1", Kind: "TPROXY", Network: "udp", AddressFamily: "ipv4",
			IngressScopeProviderID: "network-owner", IngressScopeID: "scope:eth0",
			IngressScopeRevision: digest, InterceptionFactRevision: digest, RuntimeRevision: digest,
			ListenerRevision: digest, ManagedCandidateRevision: digest, PolicyRoutingRevision: digest,
			ManagementExclusionRevision: digest, HealthTransactionRevision: digest,
		},
		OperationID: "operation-1", MarkerRevision: digest, RuntimeGeneration: digest,
		Challenge: "challenge-1", MarkerAt: time.Now().UTC().Unix() - 3, DeadlineAt: time.Now().UTC().Unix() + 10,
	}
}

func TestInterceptionProbeRequiresFreshRequestResponseAndAllSafetyDimensions(t *testing.T) {
	for name, mutate := range map[string]func(*InterceptionProbeObservationV1){
		"pre_marker": func(value *InterceptionProbeObservationV1) { value.StartedAt -= 10 },
		"send_only":  func(value *InterceptionProbeObservationV1) { value.ResponseReceived = false },
		"wrong_sink": func(value *InterceptionProbeObservationV1) { value.WrongSinkUntouched = false },
		"destination": func(value *InterceptionProbeObservationV1) {
			value.OriginalDestinationPreserved = false
		},
		"source":     func(value *InterceptionProbeObservationV1) { value.SourcePreserved = false },
		"management": func(value *InterceptionProbeObservationV1) { value.ManagementPreserved = false },
		"udp_expiry": func(value *InterceptionProbeObservationV1) { value.BoundedFlowExpired = false },
		"generation": func(value *InterceptionProbeObservationV1) { value.RuntimeGeneration = hostresources.Revision("old") },
		"challenge":  func(value *InterceptionProbeObservationV1) { value.Challenge = "replayed" },
	} {
		t.Run(name, func(t *testing.T) {
			registry := NewInterceptionProbeRegistryV1()
			if _, err := registry.Register(interceptionProbeFixture{id: "probe", mutate: mutate}); err != nil {
				t.Fatal(err)
			}
			if _, err := registry.Probe(t.Context(), interceptionProbeRequestFixture()); err == nil {
				t.Fatal("unsafe observation passed")
			}
		})
	}
}

func TestInterceptionProbeRejectsAbsentAndAmbiguousProviders(t *testing.T) {
	request := interceptionProbeRequestFixture()
	registry := NewInterceptionProbeRegistryV1()
	if _, err := registry.Probe(t.Context(), request); err == nil {
		t.Fatal("absent probe provider passed")
	}
	if _, err := registry.Register(interceptionProbeFixture{id: "probe-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register(interceptionProbeFixture{id: "probe-b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Probe(t.Context(), request); err == nil {
		t.Fatal("ambiguous probe providers passed")
	}
}

func TestInterceptionProbeAcceptsExactPostMarkerUDPTransaction(t *testing.T) {
	registry := NewInterceptionProbeRegistryV1()
	if _, err := registry.Register(interceptionProbeFixture{id: "probe"}); err != nil {
		t.Fatal(err)
	}
	observation, err := registry.Probe(t.Context(), interceptionProbeRequestFixture())
	if err != nil || !observation.ResponseReceived || !observation.BoundedFlowExpired {
		t.Fatalf("observation=%#v err=%v", observation, err)
	}
}
