package resources

import (
	"strings"
	"testing"
	"time"
)

func frontingFactFixture(t *testing.T, now time.Time, address string, family AddressFamily) FrontingBackendFactV1 {
	t.Helper()
	return frontingFactFixtureForKind(t, now, address, family, string(FrontingBackendInboundResource))
}

func frontingFactFixtureForKind(t *testing.T, now time.Time, address string, family AddressFamily, kind string) FrontingBackendFactV1 {
	t.Helper()
	endpoint := PublicEndpoint{Schema: EndpointSchemaV1, ID: "endpoint",
		Key:    PublicEndpointKey{Network: NetworkTCP, AddressFamily: family, BindAddress: address, Port: 9443},
		Intent: EndpointIntentLocal, Protocol: "tcp", ProxyProtocol: CapabilityYes,
		ResourceID: "core:inbound:1", Owner: "core", OwnerRevision: "owner-v1",
		ConfigurationRevision: strings.Repeat("d", 64), ObservedAt: now.Unix(), Source: "fixture", ConfidenceBP: 10_000}
	resource := ProtectableResource{ID: endpoint.ResourceID, Kind: kind, Owner: endpoint.Owner,
		Capabilities: ProtectableResourceCapabilities{Known: true, OwnerRevision: endpoint.OwnerRevision}, Endpoints: []PublicEndpoint{endpoint}}
	fact, err := NewFrontingBackendFactV1(FrontingBackendFactV1{
		ProviderID: "provider", ContributorID: "contributor", ProviderRevision: "provider-v1",
		HealthRevision: strings.Repeat("b", 64), CapacityRevision: strings.Repeat("c", 64),
		Ownership: FrontingBackendProviderManaged, CanReachManagement: CapabilityNo, HealthReady: true, CapacityReady: true,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(),
	}, resource, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	return fact
}
