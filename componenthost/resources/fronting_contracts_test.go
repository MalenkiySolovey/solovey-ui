package resources

import (
	"encoding/json"
	"net/netip"
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
			encoded, _ := json.Marshal(first)
			for _, forbidden := range []string{test.address, "9443", "hostname", "proxy_pass", "path"} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("reference leaked %q: %s", forbidden, encoded)
				}
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

func TestFrontingBackendReferenceRevisionIgnoresOnlyObservationTime(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	firstFact := frontingFactFixture(t, now, "127.0.0.1", AddressFamilyIPv4)
	refreshedFact := frontingFactFixture(t, now.Add(time.Second), "127.0.0.1", AddressFamilyIPv4)
	first, err := ReferenceFrontingBackendV1(firstFact, ProxyModeOff, now)
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := ReferenceFrontingBackendV1(refreshedFact, ProxyModeOff, now.Add(time.Second))
	if err != nil || first != refreshed {
		t.Fatalf("observation refresh changed exact reference: first=%#v refreshed=%#v err=%v", first, refreshed, err)
	}
	changedFact := refreshedFact
	changedFact.HealthRevision = Revision("changed-health")
	changed, err := ReferenceFrontingBackendV1(changedFact, ProxyModeOff, now.Add(time.Second))
	if err != nil || changed.CanonicalReferenceRevision == first.CanonicalReferenceRevision {
		t.Fatalf("health change retained exact reference: changed=%#v err=%v", changed, err)
	}
}

func TestFrontingBackendReferenceRejectsStalePublicManagementAndProxyMismatch(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	fact := frontingFactFixture(t, now, "127.0.0.1", AddressFamilyIPv4)
	reference, _ := ReferenceFrontingBackendV1(fact, ProxyModeOff, now)
	stale := fact
	stale.ProviderRevision = "provider-v2"
	if ResolveExactFrontingBackendV1(reference, stale, now) == nil || ResolveExactFrontingBackendV1(reference, fact, fact.ExpiresAtTime()) == nil {
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

func (f FrontingBackendFactV1) ExpiresAtTime() time.Time { return time.Unix(f.ExpiresAt, 0) }

func TestEndpointLeasePureCASAndTransitionRules(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	reference, _ := ReferenceFrontingBackendV1(frontingFactFixture(t, now, "127.0.0.1", AddressFamilyIPv4), ProxyModeOff, now)
	current, err := FinalizeEndpointLeaseV1(EndpointLeaseV1{LeaseID: "lease", AuthorityProviderID: reference.ProviderID, HolderID: "holder",
		ExactReference: reference, State: EndpointLeaseReserved, IssuedAt: now.Unix(), RenewedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	next := current
	next.State, next.RenewedAt, next.ExpiresAt = EndpointLeaseMutationPending, now.Add(time.Second).Unix(), now.Add(2*time.Minute).Unix()
	next, err = FinalizeEndpointLeaseV1(next)
	if err != nil {
		t.Fatal(err)
	}
	cas := EndpointLeaseCASV1{RequestID: "request", LeaseID: current.LeaseID, ExpectedRevision: current.LeaseRevision}
	if err := ValidateEndpointLeaseTransitionV1(current, next, cas, EndpointLeaseFence, now); err != nil {
		t.Fatal(err)
	}
	cas.ExpectedRevision = strings.Repeat("f", 64)
	if ValidateEndpointLeaseTransitionV1(current, next, cas, EndpointLeaseFence, now) == nil {
		t.Fatal("stale lease CAS was accepted")
	}
}

func TestFrontingBackendEndpointRequiresExactProviderFact(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	fact := frontingFactFixture(t, now, "127.0.0.1", AddressFamilyIPv4)
	reference, err := ReferenceFrontingBackendV1(fact, ProxyModeOff, now)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := ResolveFrontingBackendEndpointV1(reference, fact, now)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Address != netip.MustParseAddr("127.0.0.1") || endpoint.Port != 9443 || endpoint.Network != NetworkTCP || endpoint.AddressFamily != AddressFamilyIPv4 {
		t.Fatalf("unexpected execution endpoint: %#v", endpoint)
	}
	stale := fact
	stale.ProviderRevision = "provider-v2"
	if _, err := ResolveFrontingBackendEndpointV1(reference, stale, now); err == nil {
		t.Fatal("stale provider fact yielded an execution endpoint")
	}
}

func TestEndpointLeaseProviderRequestsAreBoundedAndExact(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	reference, err := ReferenceFrontingBackendV1(frontingFactFixture(t, now, "127.0.0.1", AddressFamilyIPv4), ProxyModeOff, now)
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	acquire := AcquireEndpointLeaseRequestV1{RequestID: "request", HolderID: "holder", Purpose: EndpointLeasePurposeL4FrontingV1,
		ExactReference: reference, FreshnessSeconds: 60}
	if err := acquire.Validate(); err != nil {
		t.Fatal(err)
	}
	acquire.Purpose = "HTTP_FRONTING"
	if acquire.Validate() == nil {
		t.Fatal("unknown lease purpose was accepted")
	}
	if err := (MutateEndpointLeaseRequestV1{RequestID: "request", LeaseID: "lease", ExpectedRevision: digest, FreshnessSeconds: 60}).Validate(true); err != nil {
		t.Fatal(err)
	}
	if err := (MutateEndpointLeaseRequestV1{RequestID: "request", LeaseID: "lease", ExpectedRevision: digest}).Validate(false); err != nil {
		t.Fatal(err)
	}
	if err := (ReleaseEndpointLeaseRequestV1{RequestID: "request", LeaseID: "lease", ExpectedRevision: digest, DetachmentRevision: digest}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (GetEndpointLeaseRequestV1{LeaseID: "lease"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (ListEndpointLeasesRequestV1{HolderID: "holder", Limit: MaxEndpointLeasePageV1}).Validate(); err != nil {
		t.Fatal(err)
	}
	if (ListEndpointLeasesRequestV1{HolderID: "holder", Limit: MaxEndpointLeasePageV1 + 1}).Validate() == nil {
		t.Fatal("oversized lease page was accepted")
	}
}
