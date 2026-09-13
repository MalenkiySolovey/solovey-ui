package resources

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func TestClaimMatchesSemanticOwnerWithoutSelectingSupervisor(t *testing.T) {
	expected := hostresources.ExpectedApplicationOwnerV1{
		Schema: hostresources.ExpectedApplicationOwnerSchemaV1, ContractRevision: strings.Repeat("a", 64),
		InstanceID: "00112233-4455-4677-8899-aabbccddeeff", SourceRevision: "src-" + strings.Repeat("1", 64),
		ArtifactRevision: "art-" + strings.Repeat("2", 64), DeploymentID: "dep-" + strings.Repeat("3", 64),
		RuntimeRootBindingRevision: strings.Repeat("4", 64), ServiceIdentity: "solovey-ui-panel",
		ExecutablePath:   "/usr/lib/solovey-ui/solovey-ui",
		ExecutableSHA256: strings.Repeat("5", 64), ProcessUID: 997, ProcessGID: 997,
	}
	resource := hostresources.ProtectableResource{Capabilities: hostresources.ProtectableResourceCapabilities{ExpectedApplicationOwner: expected}}
	uid, gid := 997, 997
	claim := SocketClaim{
		OwnerObservationRevision: strings.Repeat("6", 64), OwnerContractRevision: expected.ContractRevision,
		InstanceID: expected.InstanceID, SourceRevision: expected.SourceRevision, ArtifactRevision: expected.ArtifactRevision,
		DeploymentID: expected.DeploymentID, RuntimeRootBindingRevision: expected.RuntimeRootBindingRevision,
		ExpectedExecutableSHA256: expected.ExecutableSHA256, ServiceIdentity: expected.ServiceIdentity,
		OwnerRevision: strings.Repeat("7", 64), ConfigurationRevision: strings.Repeat("8", 64),
		Process: hostsurface.ProcessFact{UID: &uid, GID: &gid, Executable: expected.ExecutablePath, ExeDigest: expected.ExecutableSHA256},
		Service: hostsurface.ServiceFact{ProcdService: "solovey-ui", ProcdInstance: "panel"},
	}
	if !claimMatchesResourceOwner(claim, resource) {
		t.Fatalf("matching procd owner claim was rejected: %#v", claim)
	}
	claim.DeploymentID = "dep-" + strings.Repeat("9", 64)
	if claimMatchesResourceOwner(claim, resource) {
		t.Fatal("semantic deployment drift was accepted")
	}
}

func TestExactSSHExternalResourceReachesGraphWithoutReinference(t *testing.T) {
	now := time.Unix(8_000, 0).UTC()
	ownerRevision, configuration := strings.Repeat("a", 64), strings.Repeat("b", 64)
	resource := hostresources.ProtectableResource{
		ID: "ssh-management:0123456789abcdef0123456789abcdef", Kind: "ssh_management", Owner: "ssh-management",
		Name: "SSH management 22", Protocol: "tcp", Listen: "*", Port: 22, Public: true, Source: "ssh-management:listener-authority",
		Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, OwnerRevision: ownerRevision, ConfigRevision: configuration},
	}
	resource.ListenIntent = hostresources.BuildConfiguredListenIntent(resource)
	resource.ListenIntent.Mode = hostresources.ListenIntentDualStack
	resource.ListenIntent.RequiredFamilies = []hostresources.AddressFamily{hostresources.AddressFamilyIPv4, hostresources.AddressFamilyIPv6}
	resource.Endpoints = []hostresources.PublicEndpoint{
		{Schema: hostresources.EndpointSchemaV1, ID: "endpoint:ssh:ipv4", Key: hostresources.PublicEndpointKey{Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4, BindAddress: "0.0.0.0", Port: 22}, Intent: hostresources.EndpointIntentPublic, Protocol: "tcp", ResourceID: resource.ID, Owner: resource.Owner, OwnerRevision: ownerRevision, ConfigurationRevision: configuration, ObservedAt: now.Unix(), Source: resource.Source, ConfidenceBP: 10000},
		{Schema: hostresources.EndpointSchemaV1, ID: "endpoint:ssh:ipv6", Key: hostresources.PublicEndpointKey{Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv6, BindAddress: "::", Port: 22}, Intent: hostresources.EndpointIntentPublic, Protocol: "tcp", ResourceID: resource.ID, Owner: resource.Owner, OwnerRevision: ownerRevision, ConfigurationRevision: configuration, ObservedAt: now.Unix(), Source: resource.Source, ConfidenceBP: 10000},
	}
	pid, parent, session, root := 933, 1, 933, 0
	ipv6Only := false
	surface := hostsurface.HostSurfaceFactV1{
		Schema: hostsurface.SchemaV1, ID: "hostsurface:ssh", Network: hostsurface.NetworkTCP, Family: hostsurface.FamilyIPv6,
		Bind: "::", Port: 22, Exposure: hostsurface.ExposurePublic, SocketInode: "22", SocketCookie: 42,
		Process: hostsurface.ProcessFact{ProviderRevision: "process-evidence/v2", EvidenceRevision: strings.Repeat("c", 64), PID: &pid, ParentPID: &parent,
			SessionID: &session, StartTime: "123", ExeDigest: strings.Repeat("d", 64), Executable: "/usr/sbin/dropbear", ExeDevice: 8, ExeInode: 42, UID: &root, GID: &root},
		Service: hostsurface.ServiceFact{MainPID: &pid, ActiveState: "active", SubState: "running", ProcdService: "dropbear", ProcdInstance: "instance1",
			ProcdCommand: []string{"/usr/sbin/dropbear", "-F", "-P", "/var/run/dropbear.main.pid", "-p", "22"}},
		SemanticOwner: &hostsurface.SemanticOwnerFactV1{
			ManagementService: hostsurface.ManagementServiceSSH, Source: "sshbroker:listener-authority", Revision: ownerRevision,
			AuthorityRevision: strings.Repeat("e", 64), Socket: hostsurface.ListenerSocketIdentityV1{Network: hostsurface.NetworkTCP, Family: hostsurface.FamilyIPv6,
				Bind: "::", Port: 22, Inode: "22", Cookie: 42, Wildcard: true, IPv6Only: &ipv6Only, CoverageFamilies: []hostsurface.Family{hostsurface.FamilyIPv4, hostsurface.FamilyIPv6}},
		},
		RegisteredResourceID: resource.ID, DesiredOwner: resource.Owner, OwnershipMode: hostsurface.OwnershipExternalManaged,
		FirstSeen: now.Unix(), LastSeen: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(), Source: "sshbroker:listener-authority",
		ConfidenceBP: 10000, ConfigurationRevision: configuration, Classification: hostsurface.ClassificationExpectedExternal,
		ReasonCodes: []string{"ssh_listener_authority_exact"},
	}
	graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{surface}, Now: now})
	if len(graph.Nodes) != 1 || len(graph.Nodes[0].ObservedClaims) != 2 || graph.Nodes[0].ObservedClaims[0].Ambiguous || graph.Nodes[0].ObservedClaims[1].Ambiguous ||
		!containsGraphReason(graph.Nodes[0].ReasonCodes, "listener_external_managed") || containsGraphReason(graph.Nodes[0].ReasonCodes, "listener_deployment_mismatch") || containsGraphReason(graph.Nodes[0].ReasonCodes, "endpoint_address_family_unresolved") {
		t.Fatalf("exact external SSH resource was downgraded or re-inferred: %#v", graph)
	}
	evidence, err := BuildSocketOwnershipGraphEvidence(graph, []hostresources.ProtectableResource{resource}, []hostsurface.HostSurfaceFactV1{surface}, now)
	if err != nil {
		t.Fatalf("exact external SSH evidence was not frozen: %v", err)
	}
	owner := evidence.Nodes[0].OwnerObservations[0]
	if !owner.SemanticOwnerCurrent || owner.SemanticOwner == nil || owner.SemanticOwner.Revision != ownerRevision || owner.ListenerOwnerCurrent || owner.Process == nil || owner.Service == nil {
		t.Fatalf("SSH semantic owner evidence was downgraded: %#v", owner)
	}
}

func containsGraphReason(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestProcdAcceptedOwnerEvidenceRoundTripsWithoutDowngrade(t *testing.T) {
	now := time.Unix(7_000, 0).UTC()
	pid, parent, session, uid, gid := 100, 1, 100, 997, 997
	command := []string{"/usr/lib/solovey-ui/solovey-broker-readiness", "--ready"}
	owner := hostsurface.ListenerOwnerFactV1{
		Schema:      hostsurface.ListenerOwnerFactSchemaV1,
		Socket:      hostsurface.ListenerSocketIdentityV1{Network: hostsurface.NetworkTCP, Family: hostsurface.FamilyIPv4, Bind: "192.0.2.10", Port: 443, Inode: "200", Cookie: 300, CoverageFamilies: []hostsurface.Family{hostsurface.FamilyIPv4}},
		Process:     hostsurface.ProcessFact{ProviderRevision: "fixture-process-evidence/v1", EvidenceRevision: strings.Repeat("b", 64), PID: &pid, ParentPID: &parent, SessionID: &session, StartTime: "400", ExeDigest: strings.Repeat("5", 64), Executable: "/usr/lib/solovey-ui/solovey-ui", ExeDevice: 10, ExeInode: 20, UID: &uid, GID: &gid},
		Service:     hostsurface.ServiceFact{SupervisorRevision: strings.Repeat("8", 64), CgroupAvailability: "unavailable", CgroupPolicy: "optional", CgroupRevision: strings.Repeat("9", 64), MainPID: &pid, ActiveState: "active", SubState: "running", ProcdService: "solovey-ui", ProcdInstance: "panel", ProcdCommand: command, ProcdUser: "solovey-ui", ProcdGroup: "solovey-ui"},
		Application: hostsurface.ListenerApplicationIdentityV1{InstanceID: "00112233-4455-4677-8899-aabbccddeeff", SourceRevision: "src-" + strings.Repeat("1", 64), ArtifactRevision: "art-" + strings.Repeat("2", 64), DeploymentID: "dep-" + strings.Repeat("3", 64), OwnerContractRevision: strings.Repeat("a", 64), RuntimeRootBindingRevision: strings.Repeat("4", 64), ExpectedExecutableSHA256: strings.Repeat("5", 64), ServiceIdentity: "solovey-ui-panel", ResourceID: "core:panel:web", ResourceOwnerRevision: strings.Repeat("7", 64), ConfigurationRevision: strings.Repeat("8", 64)},
		ObservedAt:  now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(),
	}
	owner.Seal()
	surface := hostsurface.HostSurfaceFactV1{Schema: hostsurface.SchemaV1, ID: "surface:procd", Network: owner.Socket.Network, Family: owner.Socket.Family, Bind: owner.Socket.Bind, Port: owner.Socket.Port, SocketInode: owner.Socket.Inode, SocketCookie: owner.Socket.Cookie, ListenerOwner: &owner, RegisteredResourceID: owner.Application.ResourceID, Classification: hostsurface.ClassificationManagedExact, ReasonCodes: []string{}}
	frozen, err := buildOwnerObservationEvidence(surface, now)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(frozen)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip SocketOwnerObservationEvidenceV1
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	service := roundTrip.Service
	if service == nil || service.ProofKind != "procd" || service.ProcdService != "solovey-ui" || service.ProcdInstance != "panel" ||
		strings.Join(service.ProcdCommand, "\x00") != strings.Join(command, "\x00") || service.ProcdUser != "solovey-ui" || service.ProcdGroup != "solovey-ui" ||
		roundTrip.Process == nil || roundTrip.Process.UID == nil || *roundTrip.Process.UID != uid || roundTrip.Process.GID == nil || *roundTrip.Process.GID != gid || !serviceIdentityEvidenceValid(*service) {
		t.Fatalf("procd acceptance proposition was downgraded: %#v", roundTrip)
	}
}
