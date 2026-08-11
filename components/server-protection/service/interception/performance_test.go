package interception

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

type manyFactProvider struct {
	factProvider
	facts []hostresources.InterceptionInboundFactV1
}

func (p manyFactProvider) InterceptionFactsV1(context.Context, time.Time) ([]hostresources.InterceptionInboundFactV1, error) {
	return p.facts, nil
}

func TestInspectionPlannerMaximumFixtureIsBounded(t *testing.T) {
	const count = 1000
	now := time.Unix(1_800_000_000, 0).UTC()
	facts := make([]hostresources.InterceptionInboundFactV1, 0, count)
	digest := hostresources.Revision("performance")
	for index := 0; index < count; index++ {
		fact, err := hostresources.FinalizeInterceptionFactV1(hostresources.InterceptionInboundFactV1{
			ProviderID: "core", ProviderRevision: hostresources.InterceptionProviderRevisionV1,
			ResourceID: fmt.Sprintf("inbound:%d", index+1), EndpointID: fmt.Sprintf("endpoint:%d", index+1),
			InboundDatabaseID: uint(index + 1), Kind: hostresources.InterceptionRedirectV1,
			Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4,
			ConfiguredBind: "0.0.0.0", ConfiguredPort: uint16(10000 + index%50000),
			Ownership:             hostresources.InterceptionOwnershipUnknownV1,
			ListenerState:         hostresources.InterceptionListenerUnobservedV1,
			ConfigurationRevision: digest, RuntimeRevision: digest, RuntimeGenerationRevision: digest,
			ListenerRevision: digest, CoreSemanticRevision: digest, LinuxOnly: true,
			OriginalDestinationMechanism: "SO_ORIGINAL_DST", OriginalDestinationPreserved: true,
			SourcePreserved: true, ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(),
		})
		if err != nil {
			t.Fatal(err)
		}
		facts = append(facts, fact)
	}
	registry := hostresources.NewInterceptionRegistryV1()
	if _, err := registry.Register(manyFactProvider{facts: facts}); err != nil {
		t.Fatal(err)
	}
	scopes := hostresources.NewForwardedIngressScopeRegistryV1()
	service := &Service{Interceptions: registry, IngressScopes: scopes, Now: func() time.Time { return now }, GOOS: "linux"}

	durations := make([]time.Duration, 20)
	for index := range durations {
		start := time.Now()
		status, err := service.Status(t.Context())
		if err != nil || len(status.Resources) != count {
			t.Fatalf("status facts=%d err=%v", len(status.Resources), err)
		}
		durations[index] = time.Since(start)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[18]
	if p95 > 500*time.Millisecond {
		t.Fatalf("1,000-fact inspection p95 %s exceeds 500ms control-plane budget", p95)
	}
	for _, resource := range facts {
		if resource.HealthCapabilityReady {
			t.Fatal("performance fixture accidentally made a profile actionable")
		}
	}
	t.Logf("1,000-fact inspection p95=%s; data-plane latency/throughput was not measured", p95)
}
