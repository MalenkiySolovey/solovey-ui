package health

import (
	"context"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

type localProxyProbeFixture struct {
	mutate func(LocalProxyProbeRequestV1, *LocalProxyProbeObservationV1)
}

func (*localProxyProbeFixture) ProviderID() string       { return "core-sing-box-local-proxy-probe-v1" }
func (*localProxyProbeFixture) ProviderInstance() string { return "fixture-local-proxy-instance" }
func (p *localProxyProbeFixture) Capability(_ context.Context, target LocalProxyProbeTargetV1) LocalProxyProbeCapabilityV1 {
	return FinalizeLocalProxyProbeCapabilityV1(LocalProxyProbeCapabilityV1{
		ProviderID: p.ProviderID(), ProviderInstance: p.ProviderInstance(), ResourceID: target.ResourceID,
		EndpointID: target.EndpointID, Protocol: target.Protocol, Available: true,
	})
}
func (p *localProxyProbeFixture) Probe(_ context.Context, request LocalProxyProbeRequestV1) (LocalProxyProbeObservationV1, error) {
	started := time.Now().UTC().UnixNano()
	if started <= request.NotBeforeUnixNano {
		started = request.NotBeforeUnixNano + 1
	}
	probeID := hostresources.Revision(struct {
		Challenge  string
		Generation uint64
	}{request.ChallengeRevision, request.MinimumGeneration})
	sinkRevision := hostresources.Revision(struct{ Challenge string }{request.ChallengeRevision})
	target := request.Target
	value := LocalProxyProbeObservationV1{
		ProviderID: p.ProviderID(), ProviderInstance: p.ProviderInstance(), ResourceID: target.ResourceID,
		EndpointID: target.EndpointID, Protocol: target.Protocol, ConfigurationRevision: target.ConfigurationRevision,
		RuntimeRevision: target.RuntimeRevision, FactRevision: target.FactRevision,
		ListenerObservationRevision: target.ListenerObservationRevision, AuthenticationRevision: target.AuthenticationRevision,
		TLSRevision: target.TLSRevision, SystemProxyRevision: target.SystemProxyRevision,
		LeaseID: target.LeaseID, LeaseRevision: target.LeaseRevision, LeaseState: target.LeaseState,
		OperationID: target.OperationID, OperationRevision: target.OperationRevision,
		PlanRevision: target.PlanRevision, MarkerRevision: target.MarkerRevision,
		ChallengeRevision: request.ChallengeRevision, Generation: request.MinimumGeneration, ProbeID: probeID,
		StartedUnixNano: started, CompletedUnixNano: started, ExpiresUnixNano: started + int64(30*time.Second),
		Passed: true, PositiveTransaction: true, MissingAuthenticationDenied: true,
		InvalidAuthenticationDenied: true, ExactTarget: true, ExactSink: true, SinkRevision: sinkRevision,
		ResponderRevision: hostresources.Revision(struct{ Challenge, Probe, Sink string }{request.ChallengeRevision, probeID, sinkRevision}),
	}
	if p.mutate != nil {
		p.mutate(request, &value)
	}
	return FinalizeLocalProxyProbeObservationV1(value), nil
}

func localProxyProbeRequestFixture(protocol hostresources.LocalProxyProtocolV1) LocalProxyProbeRequestV1 {
	revision := hostresources.Revision("fixture")
	return LocalProxyProbeRequestV1{
		Target: LocalProxyProbeTargetV1{
			ProviderID: "core", ResourceID: "core:inbound:17", EndpointID: "tcp:ipv4:1080", Protocol: protocol,
			ConfigurationRevision: revision, RuntimeRevision: revision, FactRevision: revision,
			ListenerObservationRevision: revision, AuthenticationRevision: revision, TLSRevision: revision,
			SystemProxyRevision: revision, LeaseID: "lease-1", LeaseRevision: revision,
			LeaseState: hostresources.EndpointLeaseMutationPending, OperationID: "operation-1", OperationRevision: 2,
			PlanRevision: revision, MarkerRevision: revision,
		},
		ProviderInstance: "fixture-local-proxy-instance", NotBeforeUnixNano: time.Now().Add(-time.Second).UnixNano(),
	}
}

func TestLocalProxyProbeAcceptsEveryExactProtocolAndUsesPerProtocolReplayKeys(t *testing.T) {
	registry := NewLocalProxyProbeRegistryV1()
	if _, err := registry.Register(&localProxyProbeFixture{}); err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []hostresources.LocalProxyProtocolV1{
		hostresources.LocalProxyProtocolSOCKS4, hostresources.LocalProxyProtocolSOCKS5,
		hostresources.LocalProxyProtocolHTTPForward, hostresources.LocalProxyProtocolHTTPConnect,
	} {
		observation, err := registry.ProbeFresh(t.Context(), localProxyProbeRequestFixture(protocol))
		if err != nil || observation.Protocol != protocol || !observation.Passed {
			t.Fatalf("%s observation=%#v err=%v", protocol, observation, err)
		}
	}
}

func TestLocalProxyProbeRejectsDriftWrongResponderAndNonExactSink(t *testing.T) {
	for name, mutate := range map[string]func(LocalProxyProbeRequestV1, *LocalProxyProbeObservationV1){
		"pre marker": func(request LocalProxyProbeRequestV1, value *LocalProxyProbeObservationV1) {
			value.StartedUnixNano, value.CompletedUnixNano = request.NotBeforeUnixNano-1, request.NotBeforeUnixNano-1
		},
		"protocol drift": func(_ LocalProxyProbeRequestV1, value *LocalProxyProbeObservationV1) {
			value.Protocol = hostresources.LocalProxyProtocolHTTPConnect
		},
		"listener drift": func(_ LocalProxyProbeRequestV1, value *LocalProxyProbeObservationV1) {
			value.ListenerObservationRevision = hostresources.Revision("drift")
		},
		"wrong responder": func(_ LocalProxyProbeRequestV1, value *LocalProxyProbeObservationV1) {
			value.ResponderRevision = hostresources.Revision("wrong")
		},
		"non exact sink": func(_ LocalProxyProbeRequestV1, value *LocalProxyProbeObservationV1) {
			value.ExactSink = false
		},
	} {
		t.Run(name, func(t *testing.T) {
			registry := NewLocalProxyProbeRegistryV1()
			if _, err := registry.Register(&localProxyProbeFixture{mutate: mutate}); err != nil {
				t.Fatal(err)
			}
			if _, err := registry.ProbeFresh(t.Context(), localProxyProbeRequestFixture(hostresources.LocalProxyProtocolSOCKS5)); err == nil {
				t.Fatal("invalid observation was accepted")
			}
		})
	}
}

func TestLocalProxyProbeRejectsGenerationAndProofReplay(t *testing.T) {
	provider := &localProxyProbeFixture{}
	registry := NewLocalProxyProbeRegistryV1()
	if _, err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	request := localProxyProbeRequestFixture(hostresources.LocalProxyProtocolSOCKS5)
	first, err := registry.ProbeFresh(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	provider.mutate = func(_ LocalProxyProbeRequestV1, value *LocalProxyProbeObservationV1) {
		value.Generation, value.ProbeID = first.Generation, first.ProbeID
		value.ResponderRevision = hostresources.Revision(struct{ Challenge, Probe, Sink string }{value.ChallengeRevision, value.ProbeID, value.SinkRevision})
	}
	if _, err := registry.ProbeFresh(t.Context(), request); err == nil {
		t.Fatal("replayed proof was accepted")
	}
}
