package resources

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

type testContributor struct {
	owner string
	items []ProtectableResource
	err   error
	calls int
}

type blockingContributor struct {
	panic bool
}

func (blockingContributor) Owner() string { return "blocking" }
func (contributor blockingContributor) ListProtectableResources(context.Context) ([]ProtectableResource, error) {
	if contributor.panic {
		panic("secret")
	}
	time.Sleep(250 * time.Millisecond)
	return nil, nil
}

func registerTestContributor(t *testing.T, registry *Registry, contributor ResourceContributor) func() {
	t.Helper()
	unregister, err := registry.Register(contributor)
	if err != nil {
		t.Fatal(err)
	}
	return unregister
}

func (c *testContributor) Owner() string { return c.owner }
func (c *testContributor) ListProtectableResources(context.Context) ([]ProtectableResource, error) {
	c.calls++
	return append([]ProtectableResource(nil), c.items...), c.err
}

func TestNormalizeListen(t *testing.T) {
	tests := []struct {
		input  string
		value  string
		class  ListenClass
		family int
	}{
		{"", "*", ListenWildcard, 0},
		{"0.0.0.0", "0.0.0.0", ListenIPv4Wildcard, 4},
		{"[::]", "::", ListenIPv6Wildcard, 6},
		{"127.0.0.1", "127.0.0.1", ListenLoopback, 4},
		{"::ffff:127.0.0.1", "127.0.0.1", ListenLoopback, 4},
		{"[::1]", "::1", ListenLoopback, 6},
		{"203.0.113.7", "203.0.113.7", ListenPublicExact, 4},
		{"Example.COM.", "example.com", ListenHostname, 0},
	}
	for _, test := range tests {
		got := NormalizeListen(test.input)
		if got.Value != test.value || got.Class != test.class || got.Family != test.family {
			t.Fatalf("NormalizeListen(%q) = %#v", test.input, got)
		}
	}
}

func TestDeterministicConfiguredEndpointKeysPreserveWildcardFamiliesWithoutOwnerProof(t *testing.T) {
	resource := ProtectableResource{
		ID: "fixture:wildcard", Kind: "panel_web", Protocol: "stream", Listen: "[::]", Port: 443,
		Capabilities: ProtectableResourceCapabilities{ConfigRevision: strings.Repeat("c", 64)},
	}
	resource.ListenIntent = BuildConfiguredListenIntent(resource)
	keys, complete := DeterministicConfiguredEndpointKeys(resource)
	if !complete || len(keys) != 2 || keys[0].AddressFamily != AddressFamilyIPv4 || keys[0].BindAddress != "0.0.0.0" || keys[1].AddressFamily != AddressFamilyIPv6 || keys[1].BindAddress != "::" {
		t.Fatalf("dual-family wildcard preservation = %#v, complete=%t", keys, complete)
	}
	resource.Capabilities.ExpectedListenerOwner = ExpectedListenerOwnerV1{}
	resource.Capabilities.OwnerRevision = ""
	if second, ok := DeterministicConfiguredEndpointKeys(resource); !ok || !reflect.DeepEqual(keys, second) {
		t.Fatalf("listener ownership changed configuration-only endpoint claims: %#v, complete=%t", second, ok)
	}
}

func TestFingerprintIsStableAndIgnoresDisplayName(t *testing.T) {
	base := ProtectableResource{
		Kind: "inbound", Owner: "core", Name: "before", Protocol: "stream", Listen: "[::1]", Port: 443, TLS: true,
		Capabilities: ProtectableResourceCapabilities{PublicHostnames: []string{"B.example", "a.example", "a.example"}},
	}
	left := Fingerprint(base)
	base.Name = "after"
	base.Capabilities.PublicHostnames = []string{"a.example", "b.example"}
	if right := Fingerprint(base); left != right {
		t.Fatalf("display-only change altered fingerprint: %s != %s", left, right)
	}
	base.Port++
	if right := Fingerprint(base); left == right {
		t.Fatal("listener change did not alter fingerprint")
	}
}

func TestRegistryCachesRefreshesAndClones(t *testing.T) {
	registry := NewRegistry(time.Minute)
	contributor := &testContributor{owner: "fixture", items: []ProtectableResource{{
		ID: "fixture:one", Kind: "component_listener", Name: "one", Protocol: "stream", Listen: "127.0.0.1", Port: 1234,
		Source: "fixture", Capabilities: ProtectableResourceCapabilities{Known: true},
		Endpoints:           []PublicEndpoint{{ID: "provider-owned", Key: PublicEndpointKey{Network: NetworkTCP, AddressFamily: AddressFamilyIPv4, BindAddress: "127.0.0.1", Port: 1234}, ObservedAt: time.Now().Unix()}},
		AdvertisedEndpoints: []AdvertisedEndpoint{{ID: "advertised:one", HostnameOrIP: "example.com", Port: 443, Network: NetworkTCP, ProtocolLabel: "https", RouteSelectors: []string{"route:one"}, SocketClaimIDs: []string{"claim:one"}}},
	}}}
	unregister := registerTestContributor(t, registry, contributor)
	first := registry.Snapshot(context.Background())
	second := registry.Snapshot(context.Background())
	if contributor.calls != 1 || len(first.Resources) != 1 || len(second.Resources) != 1 {
		t.Fatalf("unexpected cache state: calls=%d first=%#v second=%#v", contributor.calls, first, second)
	}
	if contributor.items[0].Endpoints[0].ID != "provider-owned" {
		t.Fatalf("neutral registry mutated contributor-owned endpoint: %#v", contributor.items[0].Endpoints[0])
	}
	first.Resources[0].Name = "mutated"
	first.Resources[0].AdvertisedEndpoints[0].RouteSelectors[0] = "route:mutated"
	first.Resources[0].AdvertisedEndpoints[0].SocketClaimIDs[0] = "claim:mutated"
	if got := registry.Snapshot(context.Background()).Resources[0].Name; got != "one" {
		t.Fatalf("cached snapshot was mutable: %q", got)
	}
	gotAdvertised := registry.Snapshot(context.Background()).Resources[0].AdvertisedEndpoints[0]
	if gotAdvertised.RouteSelectors[0] != "route:one" || gotAdvertised.SocketClaimIDs[0] != "claim:one" {
		t.Fatalf("cached advertised endpoint was mutable: %#v", gotAdvertised)
	}
	registry.Refresh(context.Background())
	if contributor.calls != 2 {
		t.Fatalf("refresh calls = %d", contributor.calls)
	}
	unregister()
	if got := registry.Snapshot(context.Background()); len(got.Resources) != 0 {
		t.Fatalf("resources remained after unregister: %#v", got.Resources)
	}
}

func TestRegistryContainsBlockingAndPanickingContributors(t *testing.T) {
	for name, contributor := range map[string]ResourceContributor{
		"blocking":  blockingContributor{},
		"panicking": blockingContributor{panic: true},
	} {
		t.Run(name, func(t *testing.T) {
			registry := NewRegistry(0)
			registerTestContributor(t, registry, contributor)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			started := time.Now()
			snapshot := registry.Refresh(ctx)
			if len(snapshot.Errors) != 1 || snapshot.Errors[0].Message != "resource contributor is unavailable" {
				t.Fatalf("failure snapshot = %#v", snapshot)
			}
			if time.Since(started) > 200*time.Millisecond {
				t.Fatal("contributor escaped the inventory deadline")
			}
		})
	}
}

func TestRegistryIsolatesErrorsAndDuplicateIDs(t *testing.T) {
	registry := NewRegistry(time.Minute)
	registerTestContributor(t, registry, &testContributor{owner: "broken", err: errors.New(`open C:\\secret\\token.db: offline`)})
	registerTestContributor(t, registry, &testContributor{owner: "first", items: []ProtectableResource{{ID: "same", Kind: "inbound", Protocol: "stream", Port: 1, Source: "first"}}})
	registerTestContributor(t, registry, &testContributor{owner: "second", items: []ProtectableResource{{ID: "same", Kind: "inbound", Protocol: "stream", Port: 2, Source: "second"}}})
	snapshot := registry.Refresh(context.Background())
	if len(snapshot.Errors) != 1 || snapshot.Errors[0].Owner != "broken" {
		t.Fatalf("errors = %#v", snapshot.Errors)
	}
	if snapshot.Errors[0].Message != "resource contributor is unavailable" {
		t.Fatalf("raw contributor error crossed neutral boundary: %#v", snapshot.Errors[0])
	}
	if len(snapshot.Resources) != 1 || snapshot.Resources[0].Owner != "first" {
		t.Fatalf("resources = %#v", snapshot.Resources)
	}
	codes := make([]string, 0, len(snapshot.Warnings))
	for _, warning := range snapshot.Warnings {
		codes = append(codes, warning.Code)
	}
	if !reflect.DeepEqual(codes, []string{"duplicate_resource_id"}) {
		t.Fatalf("warning codes = %#v", codes)
	}
}

func TestRegistryRejectsPathShapedIDsAndSanitizesEndpointIdentity(t *testing.T) {
	registry := NewRegistry(time.Minute)
	registerTestContributor(t, registry, &testContributor{owner: "fixture", items: []ProtectableResource{
		{ID: `/var/lib/private/token`, Kind: "inbound", Protocol: "stream", Port: 443},
		{ID: "fixture:safe", Kind: "inbound", Owner: "fixture", Protocol: "stream", Listen: "127.0.0.1", Port: 443, Source: "fixture", Capabilities: ProtectableResourceCapabilities{Known: true}, Endpoints: []PublicEndpoint{{ID: `/secret/endpoint`, Key: PublicEndpointKey{Network: NetworkTCP, AddressFamily: AddressFamilyIPv4, BindAddress: `C:\\secret\\bind`, Port: 443}, ResourceID: `/secret/resource`, Source: `/secret/source`, ObservedAt: 100}}},
	}})
	snapshot := registry.Refresh(context.Background())
	if len(snapshot.Resources) != 1 || snapshot.Resources[0].ID != "fixture:safe" {
		t.Fatalf("unsafe resource ID was retained: %#v", snapshot.Resources)
	}
	endpoint := snapshot.Resources[0].Endpoints[0]
	if strings.Contains(endpoint.ID+endpoint.ResourceID+endpoint.Source+endpoint.Key.BindAddress, "secret") || endpoint.Known() {
		t.Fatalf("unsafe endpoint identity was exposed or accepted: %#v", endpoint)
	}
}

func TestRegistryBoundsCardinalityAndMarksTruncation(t *testing.T) {
	registry := NewRegistry(time.Minute)
	items := make([]ProtectableResource, MaxResourceFacts+1)
	for index := range items {
		items[index] = ProtectableResource{
			ID:           fmt.Sprintf("fixture:resource:%d", index),
			Kind:         "inbound",
			Protocol:     "stream",
			Listen:       "127.0.0.1",
			Port:         443,
			Source:       "fixture",
			Capabilities: ProtectableResourceCapabilities{Known: true},
		}
	}
	registerTestContributor(t, registry, &testContributor{owner: "fixture", items: items})

	snapshot := registry.Refresh(context.Background())
	if len(snapshot.Resources) != MaxResourceFacts {
		t.Fatalf("resource cardinality = %d, want %d", len(snapshot.Resources), MaxResourceFacts)
	}
	if len(snapshot.Errors) != 1 || snapshot.Errors[0].Message != "resource inventory is truncated" {
		t.Fatalf("truncated inventory did not fail closed: %#v", snapshot.Errors)
	}
}

func TestEndpointFactsPreserveNetworkFamilyIntentAndUnknown(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	for _, test := range []struct {
		name    string
		listen  string
		port    int
		network Network
		family  AddressFamily
		intent  EndpointIntent
		known   bool
	}{
		{"tcp ipv4 wildcard", "0.0.0.0", 443, NetworkTCP, AddressFamilyIPv4, EndpointIntentPublic, true},
		{"udp ipv6 wildcard", "::", 443, NetworkUDP, AddressFamilyIPv6, EndpointIntentPublic, true},
		{"local ipv4", "127.0.0.1", 8080, NetworkTCP, AddressFamilyIPv4, EndpointIntentLocal, true},
		{"private ipv6", "fd00::1", 8443, NetworkTCP, AddressFamilyIPv6, EndpointIntentPrivate, true},
		{"unqualified wildcard", "*", 0, NetworkUnknown, AddressFamilyUnknown, EndpointIntentUnknown, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			resource := ProtectableResource{ID: "fixture", Owner: "core", Protocol: string(test.network), Listen: test.listen, Port: test.port, Source: "fixture", Capabilities: ProtectableResourceCapabilities{CanServeFallback: CapabilityUnknown}}
			fact := BuildEndpointFact(resource, test.network, now)
			if fact.Key.AddressFamily != test.family || fact.Intent != test.intent || fact.Known() != test.known || fact.ObservedAt != now.Unix() {
				t.Fatalf("endpoint fact = %#v", fact)
			}
			if !test.known && len(fact.ReasonCodes) == 0 {
				t.Fatal("unknown endpoint omitted reason codes")
			}
		})
	}
}

func TestRecoveryPathRejectsExistingConnectionAsProof(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	value := RecoveryPathV1{Schema: RecoveryPathSchemaV1, ID: "recovery:one", Kind: string(ManagementSSH), EndpointID: "management:ssh:one", PrincipalID: "principal:hash", VerificationState: "verified", VerificationMethod: "existing_connection", VerifiedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix(), IndependenceClass: "same_session", SourceRevision: strings.Repeat("a", 64), ConfigurationRevision: strings.Repeat("c", 64)}
	if RecoveryPathFresh(value, now) {
		t.Fatal("existing connection was accepted as independent recovery proof")
	}
	value.VerificationMethod = "fresh_ssh_login"
	value.IndependenceClass = "independent_reconnect"
	value.OperationBound = true
	value.SingleUse = true
	value.TargetOperation = "ssh-operation:one"
	value.Revision = 1
	value.ExpiresAt = now.Add(10 * time.Minute).Unix()
	if !RecoveryPathFresh(value, now) {
		t.Fatal("fresh independent SSH login was rejected")
	}
	value.EndpointID = ""
	if RecoveryPathFresh(value, now) {
		t.Fatal("recovery proof without an exact endpoint identity was accepted")
	}
	value.EndpointID = "management:ssh:one"
	value.ReasonCodes = []string{"stale"}
	if RecoveryPathFresh(value, now) {
		t.Fatal("recovery proof carrying unresolved reasons was accepted")
	}
}

func TestManagementEndpointCurrentRequiresExactSemanticRevision(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	value := ManagementEndpointV1{
		Schema: ManagementEndpointSchemaV1, ID: "management:ssh:one", Network: NetworkTCP,
		Family: AddressFamilyIPv4, Bind: "192.0.2.10", Port: 22, ServiceKind: ManagementSSH,
		Exposure: EndpointIntentPublic, Owner: "system", Source: "host-surface", RecoveryPolicy: "fresh_independent_path_required",
		ObservedListener: true, ObservedAt: 99, ExpiresAt: 190, ConfidenceBP: 10000, ConfigurationRevision: strings.Repeat("a", 64),
	}
	if !ManagementEndpointCurrent(value, now) {
		t.Fatal("exact current management endpoint was rejected")
	}
	value.ConfigurationRevision = "legacy-nonempty-revision"
	if ManagementEndpointCurrent(value, now) {
		t.Fatal("non-digest management revision was accepted")
	}
	value.ConfigurationRevision = strings.Repeat("a", 64)
	value.ReasonCodes = []string{"listener_owner_ambiguous"}
	if ManagementEndpointCurrent(value, now) {
		t.Fatal("ambiguous management ownership was accepted")
	}
	value.ReasonCodes = nil
	value.ExpiresAt = now.Unix()
	if ManagementEndpointCurrent(value, now) {
		t.Fatal("expired management endpoint was accepted")
	}
}

func TestRegistryBoundsNestedFactsAndRedactsPathMetadata(t *testing.T) {
	registry := NewRegistry(time.Minute)
	endpoints := make([]PublicEndpoint, MaxEndpointsPerResource+1)
	for index := range endpoints {
		endpoints[index] = PublicEndpoint{Key: PublicEndpointKey{Network: NetworkTCP, AddressFamily: AddressFamilyIPv4, BindAddress: "127.0.0.1", Port: 443}, ObservedAt: 100}
	}
	advertised := make([]AdvertisedEndpoint, MaxAdvertisedEndpointsPerResource+1)
	for index := range advertised {
		advertised[index] = AdvertisedEndpoint{ID: fmt.Sprintf("advertised:%d", index), HostnameOrIP: "example.com", Port: 443, Network: NetworkTCP, ProtocolLabel: "https"}
	}
	registry.now = func() time.Time { return time.Unix(100, 0).UTC() }
	registerTestContributor(t, registry, &testContributor{owner: "fixture", items: []ProtectableResource{{
		ID: "fixture:nested", Kind: "inbound", Name: `/private/secret`, Protocol: "stream", Listen: `C:\\private\\bind`, Port: 443, Source: "fixture", Capabilities: ProtectableResourceCapabilities{Known: true}, Endpoints: endpoints, AdvertisedEndpoints: advertised,
	}}})
	snapshot := registry.Refresh(context.Background())
	if len(snapshot.Resources) != 1 || len(snapshot.Resources[0].Endpoints) != MaxEndpointsPerResource || len(snapshot.Resources[0].AdvertisedEndpoints) != MaxAdvertisedEndpointsPerResource {
		t.Fatalf("nested resource facts were not bounded: %#v", snapshot.Resources)
	}
	payload := fmt.Sprintf("%#v", snapshot.Resources[0])
	if strings.Contains(payload, "private") || snapshot.Resources[0].Capabilities.Known || len(snapshot.Errors) == 0 {
		t.Fatalf("path-shaped or truncated metadata did not fail closed: %s errors=%#v", payload, snapshot.Errors)
	}
}
