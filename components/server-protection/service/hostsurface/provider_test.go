package hostsurface

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostmanagement "github.com/MalenkiySolovey/solovey-ui/componenthost/management"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

type recordingSSHProjection struct {
	sequence   *[]string
	projection SSHListenerProjection
	calls      int
}

func (s *recordingSSHProjection) ProjectSSHListeners(context.Context, func() time.Time) (SSHListenerProjection, error) {
	s.calls++
	*s.sequence = append(*s.sequence, "ssh_projection")
	return s.projection, nil
}

func TestNormalizeClassifiesEveryObservedListenerOrUnknown(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	managed := hostresources.ProtectableResource{ID: "core:inbound:1", Owner: "core", Protocol: "stream", Listen: "0.0.0.0", Port: 443, Source: "fixture", Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, ConfigRevision: "cfg-1"}}
	managed.Endpoints = []hostresources.PublicEndpoint{hostresources.BuildEndpointFact(managed, hostresources.NetworkTCP, now)}
	raw := PlatformSnapshot{Sockets: []RawSocket{
		{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 443, Inode: "1", Processes: []RawProcess{productionProcessFixture(10, "/usr/bin/managed", "managed")}},
		{Network: hostfacts.NetworkUDP, Family: hostfacts.FamilyIPv6, Bind: "::1", Port: 5353, Inode: "2", Processes: []RawProcess{productionProcessFixture(10, "/usr/bin/local", "local")}},
		{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 9000, Inode: "3", Processes: []RawProcess{productionProcessFixture(10, "/usr/bin/unexpected", "unexpected")}},
		{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv6, Bind: "::", Port: 22, Inode: "4"},
	}}
	result := Normalize(raw, hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{managed}}, now)
	if len(result.Facts) != 4 {
		t.Fatalf("facts = %d", len(result.Facts))
	}
	want := map[uint16]hostfacts.Classification{443: hostfacts.ClassificationUnknownOwner, 5353: hostfacts.ClassificationLocalOnly, 9000: hostfacts.ClassificationUnexpectedPublic, 22: hostfacts.ClassificationUnknownOwner}
	for _, fact := range result.Facts {
		if fact.Classification != want[fact.Port] {
			t.Fatalf("port %d classification = %s", fact.Port, fact.Classification)
		}
		if fact.ID == "" || fact.Source == "" {
			t.Fatalf("incomplete fact: %#v", fact)
		}
		if fact.Port == 443 && (fact.RegisteredResourceID != managed.ID || !containsReason(fact.ReasonCodes, "process_owner_not_verified")) {
			t.Fatalf("socket claim was mistaken for verified process ownership: %#v", fact)
		}
	}
}

func TestNormalizeMatchesEachConfiguredNetworkIntentIndependently(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	revision := strings.Repeat("c", 64)
	resource := hostresources.ProtectableResource{ID: "core:inbound:dual", Owner: "core", Protocol: "stream", Listen: "127.0.0.1", Port: 2443, Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, ConfigRevision: revision}}
	tcp := hostresources.BuildConfiguredListenIntent(resource)
	udp := tcp
	udp.Network = hostresources.NetworkUDP
	resource.ListenIntents = []hostresources.ConfiguredListenIntentV1{tcp, udp}
	result := Normalize(PlatformSnapshot{Sockets: []RawSocket{
		{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "127.0.0.1", Port: 2443},
		{Network: hostfacts.NetworkUDP, Family: hostfacts.FamilyIPv4, Bind: "127.0.0.1", Port: 2443},
	}}, hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{resource}}, now)
	if len(result.Facts) != 2 || result.Facts[0].RegisteredResourceID != resource.ID || result.Facts[1].RegisteredResourceID != resource.ID || result.Facts[0].Network == result.Facts[1].Network {
		t.Fatalf("TCP/UDP intents were conflated: %#v", result.Facts)
	}
}

func TestNormalizeDoesNotMatchUnknownRegisteredEndpoint(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	resource := hostresources.ProtectableResource{ID: "core:inbound:unknown", Owner: "core", Protocol: "stream", Listen: "0.0.0.0", Port: 443, Capabilities: hostresources.ProtectableResourceCapabilities{Known: false}}
	resource.Endpoints = []hostresources.PublicEndpoint{hostresources.BuildEndpointFact(resource, hostresources.NetworkTCP, now, "inventory_truncated")}
	result := Normalize(PlatformSnapshot{Sockets: []RawSocket{{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 443, Processes: []RawProcess{productionProcessFixture(7, "/usr/bin/unknown", "unknown")}}}}, hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{resource}}, now)
	fact := result.Facts[0]
	if fact.RegisteredResourceID != "" || fact.Classification != hostfacts.ClassificationUnexpectedPublic {
		t.Fatalf("unknown registered endpoint was accepted as current: %#v", fact)
	}
}

func containsReason(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestNormalizePreservesPIDOwnerAmbiguity(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	result := Normalize(PlatformSnapshot{Sockets: []RawSocket{{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 8443, Inode: "8", Processes: []RawProcess{{PID: 1}, {PID: 2}}}}}, hostresources.ResourceSnapshot{}, now)
	fact := result.Facts[0]
	if fact.Classification != hostfacts.ClassificationUnknownOwner || len(fact.ReasonCodes) == 0 || fact.Process.PID != nil {
		t.Fatalf("ambiguous fact = %#v", fact)
	}
}

func TestNormalizeDoesNotPrivatelyClassifyAmbiguousSupervisorSocket(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	first := RawProcess{PID: 784, UID: 0, StartTime: "20", Executable: "/fixture/daemon"}
	second := RawProcess{PID: 1, UID: 0, StartTime: "1", Executable: "/fixture/supervisor"}
	result := Normalize(PlatformSnapshot{Sockets: []RawSocket{{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 22, Inode: "22", Processes: []RawProcess{first, second}}}}, hostresources.ResourceSnapshot{}, now)
	fact := result.Facts[0]
	if fact.Process.PID != nil || !containsReason(fact.ReasonCodes, "process_owner_ambiguous") {
		t.Fatalf("generic consumer assigned private supervisor meaning: %#v", fact)
	}
}

func TestIncompleteProcessEvidenceStaysUnknownThroughRegistry(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	provider := &Provider{
		Now:       func() time.Time { return now },
		Resources: func(context.Context) hostresources.ResourceSnapshot { return hostresources.ResourceSnapshot{} },
		ObservePlatform: func(context.Context, hostfacts.Limits) (PlatformSnapshot, error) {
			return PlatformSnapshot{Sockets: []RawSocket{{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 9443, Inode: "44", Processes: []RawProcess{{PID: 44, UID: 1000, GID: 1000, StartTime: "44"}}}}}, nil
		},
	}
	registry := hostfacts.NewRegistry()
	release, err := registry.Register(provider)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	snapshot := registry.Reconcile(context.Background())
	if len(snapshot.Facts) != 1 {
		t.Fatalf("facts = %d", len(snapshot.Facts))
	}
	fact := snapshot.Facts[0]
	if fact.Classification != hostfacts.ClassificationUnknownOwner || fact.ConfidenceBP != 0 || fact.Process.ExeDigest != "" || !containsReason(fact.ReasonCodes, "process_evidence_incomplete") {
		t.Fatalf("incomplete process evidence escaped closed normalization: %#v", fact)
	}
}

func TestNormalizeDetectsExecutableObjectAndEvidenceRevisionChange(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	firstProcess := productionProcessFixture(77, "/usr/bin/managed", "first-generation")
	secondProcess := productionProcessFixture(77, "/usr/bin/managed", "second-generation")
	secondProcess.ExeInode = firstProcess.ExeInode + 1
	normalize := func(process RawProcess) hostfacts.ProcessFact {
		observation := Normalize(PlatformSnapshot{Sockets: []RawSocket{{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 9443, Inode: "77", Processes: []RawProcess{process}}}}, hostresources.ResourceSnapshot{}, now)
		if len(observation.Facts) != 1 || observation.Facts[0].ConfidenceBP != 9000 {
			t.Fatalf("complete production-shaped evidence was not retained: %#v", observation)
		}
		return observation.Facts[0].Process
	}
	first, second := normalize(firstProcess), normalize(secondProcess)
	if first.ExeInode == second.ExeInode || first.EvidenceRevision == second.EvidenceRevision || first.ExeDigest == second.ExeDigest {
		t.Fatalf("object/revision replacement was not visible: first=%#v second=%#v", first, second)
	}
}

func TestNormalizeConsumesExactSSHListenerAuthority(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	revision := strings.Repeat("a", 64)
	configuration := strings.Repeat("b", 64)
	authorityRevision := strings.Repeat("c", 64)
	pid, parent, session, uid, gid := 784, 1, 784, 0, 0
	binaryRevision := strings.Repeat("d", 64)
	authority := domain.SSHListenerAuthorityV1{
		Schema: domain.ListenerAuthoritySchemaV1, EndpointIDs: []string{"management:ssh:configured:ipv4:2222:cfg1:0123456789abcdef"}, InstanceID: "cfg1",
		Socket: hostfacts.ListenerSocketIdentityV1{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 2222,
			Inode: "22", Cookie: 42, Wildcard: true, CoverageFamilies: []hostfacts.Family{hostfacts.FamilyIPv4}},
		Process: hostfacts.ProcessFact{ProviderRevision: "process-evidence/v2", EvidenceRevision: authorityRevision, PID: &pid, ParentPID: &parent,
			SessionID: &session, StartTime: "123", ExeDigest: binaryRevision, Executable: "/usr/sbin/dropbear", ExeDevice: 8, ExeInode: 42, UID: &uid, GID: &gid},
		Service: hostfacts.ServiceFact{SupervisorRevision: revision, CgroupAvailability: "unavailable", CgroupPolicy: "optional", CgroupRevision: authorityRevision,
			MainPID: &pid, ActiveState: "active", SubState: "running", ProcdService: "dropbear", ProcdInstance: "cfg1", ProcdCommand: []string{"/usr/sbin/dropbear", "-F", "-p", "2222"}},
		BinaryRevision: binaryRevision, ServiceRevision: revision, ConfigurationRevision: configuration,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(),
	}
	authority.Seal()
	resourceID := domain.ProtectionResourceID(authority)
	projection := SSHListenerProjection{Source: "sshbroker:listener-authority", Revision: revision,
		Authorities: []domain.SSHListenerAuthorityV1{authority}, ResourceIDs: map[string]string{authority.Revision: resourceID}}
	raw := PlatformSnapshot{Sockets: []RawSocket{{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 2222, Inode: "22",
		Processes: []RawProcess{{PID: 784, UID: 0, Executable: "/fixture/accepted-owner-binary"}, {PID: 1, UID: 0, Executable: "/fixture/supervisor"}},
	}}}
	result := NormalizeWithOwnersAndSSH(raw, hostresources.ResourceSnapshot{}, nil, projection, "", now)
	fact := result.Facts[0]
	if fact.SemanticOwner == nil || fact.SemanticOwner.ManagementService != hostfacts.ManagementServiceSSH || fact.SemanticOwner.Revision != revision ||
		fact.ConfigurationRevision != configuration || fact.Process.Executable != "/usr/sbin/dropbear" || fact.SocketCookie != 42 ||
		fact.RegisteredResourceID != resourceID || fact.DesiredOwner != domain.ProtectionResourceOwner ||
		fact.ConfidenceBP != 10000 || fact.OwnershipMode != hostfacts.OwnershipExternalManaged ||
		!containsReason(fact.ReasonCodes, "ssh_listener_authority_exact") || containsReason(fact.ReasonCodes, "process_owner_ambiguous") {
		t.Fatalf("exact SSH listener authority was not consumed: %#v", fact)
	}
	fact.ID = hostfacts.StableID(fact)
	management := hostmanagement.Endpoints(nil, hostfacts.Snapshot{Facts: []hostfacts.HostSurfaceFactV1{fact}}, now)
	if len(management) != 1 || management[0].Port != 2222 || management[0].ServiceKind != hostresources.ManagementSSH ||
		!management[0].ObservedListener || management[0].ConfiguredIntent || !hostresources.ManagementEndpointCurrent(management[0], now) {
		t.Fatalf("exact SSH authority did not reach the current management inventory: %#v", management)
	}
	projection.Authorities[0].Socket.Inode = "23"
	result = NormalizeWithOwnersAndSSH(raw, hostresources.ResourceSnapshot{}, nil, projection, "", now)
	if result.Facts[0].SemanticOwner != nil {
		t.Fatalf("mismatched SSH projection was accepted: %#v", result.Facts[0])
	}
	projection.Authorities[0] = authority
	projection.Authorities[0].ExpiresAt = now.Unix()
	result = NormalizeWithOwnersAndSSH(raw, hostresources.ResourceSnapshot{}, nil, projection, "", now)
	if result.Facts[0].SemanticOwner != nil {
		t.Fatalf("expired SSH projection was accepted: %#v", result.Facts[0])
	}
}

func BenchmarkNormalize1000Facts(b *testing.B) {
	raw := PlatformSnapshot{Sockets: make([]RawSocket, 1000)}
	for i := range raw.Sockets {
		raw.Sockets[i] = RawSocket{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: uint16(10000 + i), Inode: fmt.Sprint(i), Processes: []RawProcess{productionProcessFixture(i+1, "/usr/bin/fixture", fmt.Sprint(i))}}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := Normalize(raw, hostresources.ResourceSnapshot{}, time.Unix(1_000, 0))
		if len(result.Facts) != 1000 {
			b.Fatal(len(result.Facts))
		}
	}
}

func TestProviderUsesBoundedTypedPlatformSnapshot(t *testing.T) {
	provider := &Provider{Now: func() time.Time { return time.Unix(100, 0) }, Resources: func(context.Context) hostresources.ResourceSnapshot { return hostresources.ResourceSnapshot{} }, ObservePlatform: func(_ context.Context, limits hostfacts.Limits) (PlatformSnapshot, error) {
		if limits.MaxSockets != 4096 || limits.MaxDecodedBytes != 4<<20 {
			t.Fatalf("limits = %#v", limits)
		}
		return PlatformSnapshot{Sockets: []RawSocket{{Network: hostfacts.NetworkUDP, Family: hostfacts.FamilyIPv6, Bind: "::1", Port: 53}}}, nil
	}}
	observation, err := provider.Observe(context.Background(), hostfacts.DefaultLimits())
	if err != nil || len(observation.Facts) != 1 {
		t.Fatalf("observation=%#v err=%v", observation, err)
	}
}

func TestProviderConsumesOneAdjacentSSHGenerationAfterOtherResources(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	configuration := strings.Repeat("b", 64)
	binary := strings.Repeat("d", 64)
	serviceRevision := strings.Repeat("e", 64)
	processRevision := strings.Repeat("f", 64)
	pid, parent, session, uid, gid := 784, 1, 784, 0, 0
	endpointID := "management:ssh:configured:ipv4:2222:section-A:0123456789abcdef"
	authority := domain.SSHListenerAuthorityV1{
		Schema: domain.ListenerAuthoritySchemaV1, EndpointIDs: []string{endpointID}, InstanceID: "opaque-instance-A",
		Socket: hostfacts.ListenerSocketIdentityV1{ProofMethod: hostfacts.ListenerProofProcFSV1, Network: hostfacts.NetworkTCP,
			Family: hostfacts.FamilyIPv4, Bind: "127.0.0.1", Port: 2222, Inode: "42", CoverageFamilies: []hostfacts.Family{hostfacts.FamilyIPv4}},
		Process: hostfacts.ProcessFact{ProviderRevision: "process-evidence/v2", EvidenceRevision: processRevision, PID: &pid, ParentPID: &parent,
			SessionID: &session, StartTime: "123", ExeDigest: binary, Executable: "/usr/sbin/dropbear", ExeDevice: 8, ExeInode: 42, UID: &uid, GID: &gid},
		Service: hostfacts.ServiceFact{SupervisorRevision: serviceRevision, CgroupAvailability: "unavailable", CgroupPolicy: "optional",
			CgroupRevision: processRevision, MainPID: &pid, ActiveState: "active", SubState: "running", ProcdService: "dropbear",
			ProcdInstance: "opaque-instance-A", ProcdCommand: []string{"/usr/sbin/dropbear", "-F", "-p", "2222"}},
		BinaryRevision: binary, ServiceRevision: serviceRevision, ConfigurationRevision: configuration,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(domain.MaxListenerAuthorityLifetime).Unix(),
	}
	authority.Seal()
	resourceID := domain.ProtectionResourceID(authority)
	projectedResource := hostresources.ProtectableResource{ID: resourceID, Owner: domain.ProtectionResourceOwner, Kind: domain.ProtectionResourceKind,
		Name: "SSH", Protocol: "tcp", Listen: "127.0.0.1", Port: 2222, Source: "ssh-management:listener-authority",
		Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, OwnerRevision: serviceRevision, ConfigRevision: configuration}}
	projectedResource.ListenIntent = hostresources.BuildConfiguredListenIntent(projectedResource)

	sequence := []string{}
	projection := &recordingSSHProjection{sequence: &sequence, projection: SSHListenerProjection{
		Source: "sshbroker:listener-authority", Revision: serviceRevision, Authorities: []domain.SSHListenerAuthorityV1{authority},
		ResourceIDs: map[string]string{authority.Revision: resourceID}, Resources: []hostresources.ProtectableResource{projectedResource},
	}}
	provider := &Provider{
		Now: func() time.Time { return now },
		Resources: func(context.Context) hostresources.ResourceSnapshot {
			sequence = append(sequence, "non_ssh_resources")
			return hostresources.ResourceSnapshot{GeneratedAt: now.Unix(), Resources: []hostresources.ProtectableResource{}}
		},
		SSHProjection: projection,
		ObservePlatform: func(context.Context, hostfacts.Limits) (PlatformSnapshot, error) {
			sequence = append(sequence, "platform")
			return PlatformSnapshot{Sockets: []RawSocket{{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4,
				Bind: "127.0.0.1", Port: 2222, Protocol: "tcp", Inode: "42"}}}, nil
		},
	}
	observation, err := provider.Observe(context.Background(), hostfacts.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sequence, []string{"non_ssh_resources", "ssh_projection", "platform"}) || projection.calls != 1 {
		t.Fatalf("SSH refresh was not a single adjacent generation: sequence=%#v calls=%d", sequence, projection.calls)
	}
	if len(observation.Facts) != 1 || observation.Facts[0].RegisteredResourceID != resourceID || observation.Facts[0].SemanticOwner == nil ||
		observation.Facts[0].SemanticOwner.Revision != serviceRevision || observation.Facts[0].ExpiresAt != authority.ExpiresAt {
		t.Fatalf("SSH authority/resource generation lost coherence: %#v", observation)
	}
}
