package resources

import (
	"encoding/json"
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

func TestFrontingBackendReferenceExactFreshAndRedacted(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	for _, test := range []struct {
		name, address string
		family        AddressFamily
	}{{"ipv4", "127.0.0.1", AddressFamilyIPv4}, {"ipv6", "::1", AddressFamilyIPv6}} {
		t.Run(test.name, func(t *testing.T) {
			fact := frontingFactFixture(t, now, test.address, test.family)
			first, err := ReferenceFrontingBackendV1(fact, ProxyModeOn, now)
			if err != nil {
				t.Fatal(err)
			}
			second, _ := ReferenceFrontingBackendV1(fact, ProxyModeOn, now)
			if first != second || first.CanonicalReferenceRevision == "" || ResolveExactFrontingBackendV1(first, fact, now) != nil {
				t.Fatalf("reference mismatch: %#v %#v", first, second)
			}
			encoded, err := json.Marshal(first)
			if err != nil {
				t.Fatal(err)
			}
			var decoded any
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{test.address, "hostname", "proxy_pass", "path"} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("reference leaked %q: %s", forbidden, encoded)
				}
			}
			if jsonContainsNumber(decoded, 9443) {
				t.Fatalf("reference leaked fixture backend port 9443: %s", encoded)
			}
		})
	}
	fact := frontingFactFixture(t, now, "127.0.0.1", AddressFamilyIPv4)
	payload, _ := json.Marshal(fact)
	var reconstructed FrontingBackendFactV1
	if err := json.Unmarshal(payload, &reconstructed); err != nil {
		t.Fatal(err)
	}
	if _, err := ReferenceFrontingBackendV1(reconstructed, ProxyModeOff, now); err == nil {
		t.Fatal("outward fact was reconstructed into a destination-bearing reference")
	}
}

func jsonContainsNumber(value any, wanted float64) bool {
	switch typed := value.(type) {
	case float64:
		return typed == wanted
	case []any:
		for _, item := range typed {
			if jsonContainsNumber(item, wanted) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if jsonContainsNumber(item, wanted) {
				return true
			}
		}
	}
	return false
}

func TestFrontingBackendReferenceRejectsStalePublicManagementAndProxyMismatch(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	fact := frontingFactFixture(t, now, "127.0.0.1", AddressFamilyIPv4)
	reference, _ := ReferenceFrontingBackendV1(fact, ProxyModeOff, now)
	stale := fact
	stale.ProviderRevision = "provider-v2"
	if ResolveExactFrontingBackendV1(reference, stale, now) == nil || ResolveExactFrontingBackendV1(reference, fact, time.Unix(fact.ExpiresAt, 0)) == nil {
		t.Fatal("stale exact reference was accepted")
	}
	unsafeFact := fact
	unsafeFact.CanReachManagement = CapabilityUnknown
	if _, err := ReferenceFrontingBackendV1(unsafeFact, ProxyModeOff, now); err == nil {
		t.Fatal("unknown management reachability was accepted")
	}
	unsafeFact = fact
	unsafeFact.AcceptsProxyProtocol = CapabilityNo
	if _, err := ReferenceFrontingBackendV1(unsafeFact, ProxyModeOn, now); err == nil {
		t.Fatal("PROXY mismatch was accepted")
	}
	unsafeFact = fact
	unsafeFact.Classification = EndpointClassificationPublic
	if _, err := ReferenceFrontingBackendV1(unsafeFact, ProxyModeOff, now); err == nil {
		t.Fatal("public backend was accepted")
	}
	unsafeFact = fact
	unsafeFact.Ownership = FrontingBackendExternalManaged
	if _, err := ReferenceFrontingBackendV1(unsafeFact, ProxyModeOff, now); err == nil {
		t.Fatal("external-managed backend was accepted")
	}
	for _, kind := range []string{"panel_web", "subscription", "ssh", "node_control", "external_upstream"} {
		if got := frontingFactFixtureForKindNoFail(now, "127.0.0.1", AddressFamilyIPv4, kind); got == nil {
			t.Fatalf("excluded resource kind %q was accepted", kind)
		}
	}
}

func frontingFactFixtureForKindNoFail(now time.Time, address string, family AddressFamily, kind string) error {
	endpoint := PublicEndpoint{Schema: EndpointSchemaV1, ID: "endpoint",
		Key:    PublicEndpointKey{Network: NetworkTCP, AddressFamily: family, BindAddress: address, Port: 9443},
		Intent: EndpointIntentLocal, Protocol: "tcp", ProxyProtocol: CapabilityNo,
		ResourceID: "core:resource:1", Owner: "core", OwnerRevision: "owner-v1",
		ConfigurationRevision: strings.Repeat("d", 64), ObservedAt: now.Unix(), Source: "fixture", ConfidenceBP: 10_000}
	resource := ProtectableResource{ID: endpoint.ResourceID, Kind: kind, Owner: endpoint.Owner,
		Capabilities: ProtectableResourceCapabilities{Known: true, OwnerRevision: endpoint.OwnerRevision}, Endpoints: []PublicEndpoint{endpoint}}
	_, err := NewFrontingBackendFactV1(FrontingBackendFactV1{ProviderID: "provider", ContributorID: "contributor",
		ProviderRevision: "provider-v1", HealthRevision: strings.Repeat("b", 64), CapacityRevision: strings.Repeat("c", 64),
		Ownership: FrontingBackendProviderManaged, CanReachManagement: CapabilityNo, HealthReady: true, CapacityReady: true,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()}, resource, endpoint)
	return err
}
