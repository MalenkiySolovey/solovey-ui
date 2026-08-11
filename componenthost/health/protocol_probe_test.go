package health

import (
	"context"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

type protocolProbeFixture struct {
	mutate func(ProtocolProbeRequestV1, *ProtocolProbeObservationV1)
}

func (*protocolProbeFixture) ProviderID() string       { return "fixture-protocol-probe" }
func (*protocolProbeFixture) ProviderInstance() string { return "fixture-instance-1" }
func (p *protocolProbeFixture) Capability(_ context.Context, target ProtocolProbeTargetV1) ProtocolProbeCapabilityV1 {
	return FinalizeProtocolProbeCapabilityV1(ProtocolProbeCapabilityV1{ProviderID: p.ProviderID(), ProviderInstance: p.ProviderInstance(), ResourceID: target.ResourceID, EndpointID: target.EndpointID, ProtocolClass: target.ProtocolClass, Available: true})
}
func (p *protocolProbeFixture) Probe(_ context.Context, request ProtocolProbeRequestV1) (ProtocolProbeObservationV1, error) {
	started := time.Now().UTC().UnixNano()
	if started <= request.NotBeforeUnixNano {
		started = request.NotBeforeUnixNano + 1
	}
	probeID := hostresources.Revision(struct {
		Challenge  string
		Generation uint64
	}{request.ChallengeRevision, request.MinimumGeneration})
	value := ProtocolProbeObservationV1{ProviderID: p.ProviderID(), ProviderInstance: p.ProviderInstance(), ResourceID: request.Target.ResourceID, EndpointID: request.Target.EndpointID,
		ProtocolClass: request.Target.ProtocolClass, RuntimeRevision: request.Target.RuntimeRevision, CapabilityRevision: request.Target.CapabilityRevision,
		ConfigurationRevision: request.Target.ConfigurationRevision, SocketRevision: request.Target.SocketRevision, ContributionRevision: request.ContributionRevision,
		CompositionRevision: request.CompositionRevision, ManagedPlanRevision: request.ManagedPlanRevision, ChallengeRevision: request.ChallengeRevision,
		Generation: request.MinimumGeneration, ProbeID: probeID, StartedUnixNano: started, CompletedUnixNano: started,
		ExpiresUnixNano: started + int64(30*time.Second), Passed: true, RequestResponse: true, ExactTarget: true,
		ResponderRevision: hostresources.Revision(struct{ Challenge, Probe string }{request.ChallengeRevision, probeID})}
	if p.mutate != nil {
		p.mutate(request, &value)
	}
	return FinalizeProtocolProbeObservationV1(value), nil
}

func protocolProbeRequestFixture() ProtocolProbeRequestV1 {
	revision := hostresources.Revision("fixture")
	return ProtocolProbeRequestV1{Target: ProtocolProbeTargetV1{ResourceID: "core:inbound:1", EndpointID: "udp:ipv4:443", ProtocolClass: hostresources.TransportPlainUDP,
		RuntimeRevision: revision, CapabilityRevision: revision, ConfigurationRevision: revision, SocketRevision: revision,
		AddressFamily: hostresources.AddressFamilyIPv4, ConfiguredBind: "127.0.0.1", Port: 443}, ContributionRevision: revision,
		CompositionRevision: revision, ManagedPlanRevision: revision, ProviderInstance: "fixture-instance-1", NotBeforeUnixNano: time.Now().Add(-time.Second).UnixNano()}
}

func TestProtocolProbeFreshAcceptsOnlyPostMutationRequestResponse(t *testing.T) {
	for name, mutate := range map[string]func(ProtocolProbeRequestV1, *ProtocolProbeObservationV1){
		"pre-mutation observation": func(request ProtocolProbeRequestV1, value *ProtocolProbeObservationV1) {
			value.StartedUnixNano, value.CompletedUnixNano = request.NotBeforeUnixNano-1, request.NotBeforeUnixNano-1
		},
		"send only": func(_ ProtocolProbeRequestV1, value *ProtocolProbeObservationV1) { value.RequestResponse = false },
		"wrong responder": func(_ ProtocolProbeRequestV1, value *ProtocolProbeObservationV1) {
			value.ResponderRevision = hostresources.Revision("wrong")
		},
		"runtime drift": func(_ ProtocolProbeRequestV1, value *ProtocolProbeObservationV1) {
			value.RuntimeRevision = hostresources.Revision("drift")
		},
		"socket drift": func(_ ProtocolProbeRequestV1, value *ProtocolProbeObservationV1) {
			value.SocketRevision = hostresources.Revision("drift")
		},
		"contribution drift": func(_ ProtocolProbeRequestV1, value *ProtocolProbeObservationV1) {
			value.CompositionRevision = hostresources.Revision("drift")
		},
	} {
		t.Run(name, func(t *testing.T) {
			registry := NewProtocolProbeRegistryV1()
			_, err := registry.Register(&protocolProbeFixture{mutate: mutate})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = registry.ProbeFresh(t.Context(), protocolProbeRequestFixture()); err == nil {
				t.Fatal("invalid protocol observation was accepted")
			}
		})
	}
}

func TestProtocolProbeFreshRejectsGenerationAndResultReplay(t *testing.T) {
	provider := &protocolProbeFixture{}
	registry := NewProtocolProbeRegistryV1()
	if _, err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	request := protocolProbeRequestFixture()
	first, err := registry.ProbeFresh(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	provider.mutate = func(_ ProtocolProbeRequestV1, value *ProtocolProbeObservationV1) {
		value.Generation = first.Generation
		value.ProbeID = first.ProbeID
		value.ResponderRevision = hostresources.Revision(struct{ Challenge, Probe string }{value.ChallengeRevision, value.ProbeID})
	}
	if _, err = registry.ProbeFresh(t.Context(), request); err == nil {
		t.Fatal("reused generation and result identity were accepted")
	}
}

func TestProtocolProbeSemanticRevisionIgnoresProofClockAndGeneration(t *testing.T) {
	request := protocolProbeRequestFixture()
	provider := &protocolProbeFixture{}
	request.ChallengeRevision = hostresources.Revision("challenge-one")
	request.MinimumGeneration = 1
	first, err := provider.Probe(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.ChallengeRevision = hostresources.Revision("challenge-two")
	request.MinimumGeneration = 99
	second, err := provider.Probe(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != second.Revision {
		t.Fatalf("volatile proof identity changed semantic revision: %s != %s", first.Revision, second.Revision)
	}
	second.Passed = false
	second = FinalizeProtocolProbeObservationV1(second)
	if first.Revision == second.Revision {
		t.Fatal("semantic health result did not change revision")
	}
}
