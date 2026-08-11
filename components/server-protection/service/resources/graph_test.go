package resources

import (
	"slices"
	"strings"
	"testing"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func TestSocketGraphDeterministicAndProtocolFamilyExact(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	tcp4 := graphResource("core:inbound:tcp4", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "0.0.0.0", 443)
	udp4 := graphResource("core:inbound:udp4", hostresources.NetworkUDP, hostresources.AddressFamilyIPv4, "0.0.0.0", 443)
	tcp6 := graphResource("core:inbound:tcp6", hostresources.NetworkTCP, hostresources.AddressFamilyIPv6, "::", 443)
	surfaces := []hostsurface.HostSurfaceFactV1{graphSurface(tcp4, now), graphSurface(udp4, now), graphSurface(tcp6, now)}
	input := SocketGraphInput{Resources: []hostresources.ProtectableResource{udp4, tcp6, tcp4}, Surfaces: surfaces, Now: now}
	first := BuildSocketOwnershipGraph(input)
	slices.Reverse(input.Resources)
	slices.Reverse(input.Surfaces)
	second := BuildSocketOwnershipGraph(input)
	if first.Revision != second.Revision {
		t.Fatalf("graph revision changed with input order: %s != %s", first.Revision, second.Revision)
	}
	for index := range input.Surfaces {
		input.Surfaces[index].LastSeen++
		input.Surfaces[index].ExpiresAt++
	}
	third := BuildSocketOwnershipGraph(input)
	if first.Revision != third.Revision {
		t.Fatalf("semantic graph revision changed for observation heartbeat only: %s != %s", first.Revision, third.Revision)
	}
	if len(first.Collisions) != 0 || first.ApplyBlocked {
		t.Fatalf("TCP/UDP and IPv4/IPv6 were conflated: %#v", first)
	}
	if got := first.Nodes[0].AdvertisedEndpoints[0].RouteSelectors; len(got) != 2 || got[0] != "sni:vpn.example" {
		t.Fatalf("selectors were not retained as advertised metadata: %#v", got)
	}
}

func TestSocketGraphKeepsOneResourcesTCPAndUDPIntentsIndependent(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	tcp := graphResource("core:inbound:dual", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "127.0.0.1", 2443)
	udp := graphResource("core:inbound:dual", hostresources.NetworkUDP, hostresources.AddressFamilyIPv4, "127.0.0.1", 2443)
	dual := tcp
	dual.Endpoints = []hostresources.PublicEndpoint{tcp.Endpoints[0], udp.Endpoints[0]}
	dual.ListenIntents = []hostresources.ConfiguredListenIntentV1{tcp.ListenIntent, udp.ListenIntent}
	complete := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{dual}, Surfaces: []hostsurface.HostSurfaceFactV1{graphSurface(tcp, now), graphSurface(udp, now)}, Now: now})
	if complete.ApplyBlocked || len(complete.Nodes) != 1 || len(complete.Nodes[0].DesiredClaims) != 2 || len(complete.Nodes[0].ObservedClaims) != 2 {
		t.Fatalf("dual-network resource was conflated: %#v", complete)
	}
	if complete.Nodes[0].DesiredClaims[0].Key.Network == complete.Nodes[0].DesiredClaims[1].Key.Network {
		t.Fatalf("dual-network desired claims share one network: %#v", complete.Nodes[0].DesiredClaims)
	}
	partial := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{dual}, Surfaces: []hostsurface.HostSurfaceFactV1{graphSurface(tcp, now)}, Now: now})
	if !partial.ApplyBlocked || !slices.Contains(partial.Nodes[0].ReasonCodes, "endpoint_address_family_unresolved") {
		t.Fatalf("missing UDP half was accepted: %#v", partial.Nodes[0])
	}
}

func TestSocketGraphWildcardAndObservedDualStackCollisions(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	wild := graphResource("core:inbound:wild", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "0.0.0.0", 443)
	exact := graphResource("core:inbound:exact", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "192.0.2.10", 443)
	graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{wild, exact}, Surfaces: []hostsurface.HostSurfaceFactV1{graphSurface(wild, now), graphSurface(exact, now)}, Now: now})
	if len(graph.Collisions) != 1 || graph.Collisions[0].Code != "wildcard_socket_collision" || !graph.ApplyBlocked {
		t.Fatalf("wildcard coverage did not fail closed: %#v", graph.Collisions)
	}
	for _, alternative := range graph.Collisions[0].Alternatives {
		if alternative.Code == "move_inbound" && alternative.Detail == "" {
			t.Fatal("collision alternative is not deterministic and explicit")
		}

	}

	v6 := graphResource("core:inbound:v6", hostresources.NetworkTCP, hostresources.AddressFamilyIPv6, "::", 443)
	v4 := graphResource("core:inbound:v4", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "192.0.2.10", 443)
	base := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{v6, v4}, Surfaces: []hostsurface.HostSurfaceFactV1{graphSurface(v6, now), graphSurface(v4, now)}, Now: now})
	if len(base.Collisions) != 0 {
		t.Fatal("cross-family collision was assumed without observation")
	}
	v6Claim := ""
	for _, node := range base.Nodes {
		if node.ResourceID == v6.ID {
			v6Claim = node.DesiredClaims[0].ID
		}
	}
	observed := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{v6, v4}, Surfaces: []hostsurface.HostSurfaceFactV1{graphSurface(v6, now), graphSurface(v4, now)}, DualStack: []DualStackObservation{{IPv6ClaimID: v6Claim, CoversIPv4: true, Source: "kernel-probe", SourceRevision: "probe-revision", ObservedAt: now.Unix()}}, Now: now})
	if len(observed.Collisions) != 1 || observed.Collisions[0].Code != "observed_dual_stack_collision" {
		t.Fatalf("observed dual-stack coverage was ignored: %#v", observed.Collisions)
	}
}

func TestSocketGraphUnknownOwnerAndStaleRevisionBlockApply(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	resource := graphResource("core:inbound:one", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "192.0.2.10", 443)
	surface := graphSurface(resource, now)
	surface.Classification = hostsurface.ClassificationUnknownOwner
	surface.OwnershipMode = hostsurface.OwnershipUnmanaged
	surface.DesiredOwner = ""
	graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{surface}, ExpectedRevision: map[string]string{resource.ID: "different-revision"}, Now: now})
	if !graph.ApplyBlocked || !slices.Contains(graph.Nodes[0].ReasonCodes, "socket_owner_ambiguous") || !slices.Contains(graph.Nodes[0].ReasonCodes, "stale_expected_revision") {
		t.Fatalf("unknown ownership or stale revision did not block apply: %#v", graph.Nodes[0])
	}
}

func TestSocketGraphBlocksPartialInventoryUnobservedDesiredAndForeignOwner(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	resource := graphResource("core:inbound:one", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "192.0.2.10", 443)
	unobserved := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, InventoryTruncated: true, InventoryReasonCodes: []string{"inventory_truncated"}, Now: now})
	if !unobserved.ApplyBlocked || !slices.Contains(unobserved.ReasonCodes, "hostsurface_inventory_truncated") || !slices.Contains(unobserved.Nodes[0].ReasonCodes, "desired_socket_unobserved") {
		t.Fatalf("partial or unobserved ownership was accepted: %#v", unobserved)
	}
	foreign := graphSurface(resource, now)
	foreign.RegisteredResourceID = ""
	foreign.DesiredOwner = "unknown"
	foreign.OwnershipMode = hostsurface.OwnershipUnmanaged
	foreign.Classification = hostsurface.ClassificationUnexpectedPublic
	graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{graphSurface(resource, now), foreign}, Now: now})
	if !graph.ApplyBlocked || !slices.Contains(graph.Nodes[0].ReasonCodes, "foreign_socket_collision") {
		t.Fatalf("foreign exact socket did not block mutation: %#v", graph.Nodes[0])
	}
}

func TestSocketGraphRequiresExactCurrentObservedKey(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	resource := graphResource("core:inbound:one", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "192.0.2.10", 443)
	surface := graphSurface(resource, now)
	surface.Bind = "192.0.2.11"
	graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{surface}, Now: now})
	if !graph.ApplyBlocked || !slices.Contains(graph.Nodes[0].ReasonCodes, "desired_socket_unobserved") || !slices.Contains(graph.Nodes[0].ReasonCodes, "observed_socket_drift") {
		t.Fatalf("observed key drift was accepted as exact ownership: %#v", graph.Nodes[0])
	}
}

func TestSocketGraphSharingRequiresExplicitAdapterMultiplexingContract(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	left := graphResource("core:inbound:left", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "192.0.2.10", 443)
	right := graphResource("core:inbound:right", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "192.0.2.10", 443)
	input := SocketGraphInput{Resources: []hostresources.ProtectableResource{left, right}, Surfaces: []hostsurface.HostSurfaceFactV1{graphSurface(left, now), graphSurface(right, now)}, Now: now}
	base := BuildSocketOwnershipGraph(input)
	if len(base.Collisions) != 1 {
		t.Fatalf("shared exact socket was accepted without an adapter: %#v", base.Collisions)
	}
	claimIDs := []string{base.Nodes[0].DesiredClaims[0].ID, base.Nodes[1].DesiredClaims[0].ID}
	input.Adapters = map[string]AdapterMultiplexingContract{"core:inbound:left": {AdapterID: "nginx:stream", AdapterKind: "SNI_PREREAD", OwnedClaimIDs: claimIDs, SelectorKinds: []string{"sni", "alpn"}, CapabilityRevision: "capability-revision", ConfigurationDigest: "configuration-digest"}}
	shared := BuildSocketOwnershipGraph(input)
	if len(shared.Collisions) != 0 || shared.ApplyBlocked {
		t.Fatalf("explicit adapter multiplexing contract was ignored: %#v", shared)
	}
	input.Adapters["core:inbound:left"] = AdapterMultiplexingContract{AdapterID: "nginx:stream", OwnedClaimIDs: claimIDs, SelectorKinds: []string{"sni"}}
	malformed := BuildSocketOwnershipGraph(input)
	if len(malformed.Collisions) != 1 {
		t.Fatal("incomplete adapter contract allowed socket sharing")
	}
}

func graphResource(id string, network hostresources.Network, family hostresources.AddressFamily, bind string, port uint16) hostresources.ProtectableResource {
	ownerRevision, configRevision := strings.Repeat("b", 64), strings.Repeat("c", 64)
	expected := hostresources.ExpectedListenerOwnerV1{
		Schema: hostresources.ExpectedListenerOwnerSchemaV1, ContractRevision: strings.Repeat("a", 64),
		InstanceID: "00112233-4455-4677-8899-aabbccddeeff", SourceRevision: "src-" + strings.Repeat("2", 64),
		ArtifactRevision: "art-" + strings.Repeat("3", 64), DeploymentID: "dep-" + strings.Repeat("4", 64),
		RuntimeRootBindingRevision: strings.Repeat("5", 64), ServiceIdentity: "solovey-ui.panel",
		SystemdUnit: "solovey-ui.service", ServiceFragmentPath: "/etc/systemd/system/solovey-ui.service",
		ServiceUnitSHA256: strings.Repeat("7", 64), ServiceControlGroup: "/system.slice/solovey-ui.service",
		ExecutablePath: "/usr/local/bin/solovey-ui", ExecutableSHA256: strings.Repeat("6", 64),
	}
	endpoint := hostresources.PublicEndpoint{Schema: hostresources.EndpointSchemaV1, ID: "endpoint:" + id, Key: hostresources.PublicEndpointKey{Network: network, AddressFamily: family, BindAddress: bind, Port: port}, Intent: hostresources.EndpointIntentPublic, Protocol: string(network), ResourceID: id, Owner: "sing-box", OwnerRevision: ownerRevision, ConfigurationRevision: configRevision, ObservedAt: 1000, Source: "fixture", ConfidenceBP: 10000}
	resource := hostresources.ProtectableResource{ID: id, Kind: "inbound", Owner: "sing-box", Protocol: string(network), Listen: bind, Port: int(port), Public: true, Source: "fixture", Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, OwnerRevision: ownerRevision, ConfigRevision: configRevision, ExpectedListenerOwner: expected}, Endpoints: []hostresources.PublicEndpoint{endpoint}, AdvertisedEndpoints: []hostresources.AdvertisedEndpoint{{ID: "advertised:" + id, HostnameOrIP: "vpn.example", Port: port, Network: network, RouteSelectors: []string{"sni:vpn.example", "alpn:h2"}}}}
	resource.ListenIntent = hostresources.BuildConfiguredListenIntent(resource)
	return resource
}

func graphSurface(resource hostresources.ProtectableResource, now time.Time) hostsurface.HostSurfaceFactV1 {
	endpoint := resource.Endpoints[0]
	family := hostsurface.Family(endpoint.Key.AddressFamily)
	var ipv6Only *bool
	if family == hostsurface.FamilyIPv6 {
		ipv6Only = boolPtr(true)
	}
	process := hostsurface.ProcessFact{PID: intPtr(100), ParentPID: intPtr(1), SessionID: intPtr(100), StartTime: "1000", ExeDigest: resource.Capabilities.ExpectedListenerOwner.ExecutableSHA256, Executable: "/usr/local/bin/solovey-ui", ExeDevice: 1, ExeInode: 2, UID: intPtr(0), GID: intPtr(0), ControlGroup: "/system.slice/solovey-ui.service"}
	service := hostsurface.ServiceFact{SystemdUnit: resource.Capabilities.ExpectedListenerOwner.SystemdUnit, MainPID: intPtr(100), FragmentPath: "/etc/systemd/system/solovey-ui.service", FragmentSHA256: resource.Capabilities.ExpectedListenerOwner.ServiceUnitSHA256, ActiveState: "active", SubState: "running", ControlGroup: process.ControlGroup, StartMonotonicUsec: 100}
	fact := hostsurface.ListenerOwnerFactV1{
		Schema:  hostsurface.ListenerOwnerFactSchemaV1,
		Socket:  hostsurface.ListenerSocketIdentityV1{Network: hostsurface.Network(endpoint.Key.Network), Family: family, Bind: endpoint.Key.BindAddress, Port: endpoint.Key.Port, Inode: "100", Cookie: 101, Wildcard: hostresources.NormalizeListen(endpoint.Key.BindAddress).Wildcard(), IPv6Only: ipv6Only, CoverageFamilies: []hostsurface.Family{family}},
		Process: process, Service: service,
		Application: hostsurface.ListenerApplicationIdentityV1{InstanceID: resource.Capabilities.ExpectedListenerOwner.InstanceID, SourceRevision: resource.Capabilities.ExpectedListenerOwner.SourceRevision, ArtifactRevision: resource.Capabilities.ExpectedListenerOwner.ArtifactRevision, DeploymentID: resource.Capabilities.ExpectedListenerOwner.DeploymentID, OwnerContractRevision: resource.Capabilities.ExpectedListenerOwner.ContractRevision, RuntimeRootBindingRevision: resource.Capabilities.ExpectedListenerOwner.RuntimeRootBindingRevision, ExpectedExecutableSHA256: resource.Capabilities.ExpectedListenerOwner.ExecutableSHA256, ServiceIdentity: resource.Capabilities.ExpectedListenerOwner.ServiceIdentity, ResourceID: resource.ID, ResourceOwnerRevision: resource.Capabilities.OwnerRevision, ConfigurationRevision: resource.Capabilities.ConfigRevision},
		ObservedAt:  now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(),
	}
	fact.Seal()
	return hostsurface.HostSurfaceFactV1{Schema: hostsurface.SchemaV1, ID: "surface:" + resource.ID, Network: fact.Socket.Network, Family: fact.Socket.Family, Bind: fact.Socket.Bind, Port: fact.Socket.Port, Exposure: hostsurface.ExposurePublic, SocketInode: fact.Socket.Inode, SocketCookie: fact.Socket.Cookie, Process: process, Service: service, ListenerOwner: &fact, RegisteredResourceID: resource.ID, DesiredOwner: resource.Owner, OwnershipMode: hostsurface.OwnershipManaged, FirstSeen: now.Unix(), LastSeen: now.Unix(), ExpiresAt: fact.ExpiresAt, Source: "fixture", ConfidenceBP: 10000, ConfigurationRevision: resource.Capabilities.ConfigRevision, Classification: hostsurface.ClassificationManagedExact}
}

func intPtr(value int) *int    { return &value }
func boolPtr(value bool) *bool { return &value }
