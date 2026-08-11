package resources

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func TestSocketGraphEvidencePreservesFrozenNodeOwnerAndDualStackFacts(t *testing.T) {
	now := time.Unix(8_000, 0).UTC()
	first := graphResource("fixture:evidence:a", hostresources.NetworkTCP, hostresources.AddressFamilyIPv6, "::", 2095)
	first.Listen = "*"
	first.ListenIntent = hostresources.ConfiguredListenIntentV1{Schema: hostresources.ConfiguredListenIntentSchemaV1, Mode: hostresources.ListenIntentDualStack, Network: hostresources.NetworkTCP, Address: "*", Port: 2095, RequiredFamilies: []hostresources.AddressFamily{hostresources.AddressFamilyIPv4, hostresources.AddressFamilyIPv6}, ConfigurationRevision: first.Capabilities.ConfigRevision}
	firstSurface := graphSurface(first, now)
	setOwnerSocket(&firstSurface, hostsurface.FamilyIPv6, "::", false, []hostsurface.Family{hostsurface.FamilyIPv6, hostsurface.FamilyIPv4})
	second := graphResource("fixture:evidence:b", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "192.0.2.40", 443)
	secondSurface := graphSurface(second, now)
	graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{second, first}, Surfaces: []hostsurface.HostSurfaceFactV1{secondSurface, firstSurface}, Now: now})
	before, _ := json.Marshal(graph)
	evidence, err := BuildSocketOwnershipGraphEvidence(graph, []hostresources.ProtectableResource{second, first}, []hostsurface.HostSurfaceFactV1{secondSurface, firstSurface}, now)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(graph)
	if string(before) != string(after) {
		t.Fatal("read-only graph evidence construction changed eligibility graph behavior")
	}
	if len(evidence.Nodes) != 2 || evidence.Nodes[0].ResourceID != first.ID || evidence.Nodes[1].ResourceID != second.ID {
		t.Fatalf("deterministic two-node identities were not preserved: %#v", evidence.Nodes)
	}
	node := evidence.Nodes[0]
	if len(node.Endpoints) != 1 || node.Endpoints[0].EndpointID != first.Endpoints[0].ID || len(node.DesiredClaims) != 2 {
		t.Fatalf("endpoint or family claims were lost: %#v", node)
	}
	families := []hostresources.AddressFamily{node.DesiredClaims[0].Key.AddressFamily, node.DesiredClaims[1].Key.AddressFamily}
	if !slices.Contains(families, hostresources.AddressFamilyIPv4) || !slices.Contains(families, hostresources.AddressFamilyIPv6) {
		t.Fatalf("dual-stack evidence is not family-specific: %v", families)
	}
	owner := node.OwnerObservations[0]
	if owner.Classification != hostsurface.ClassificationManagedExact || !owner.ListenerOwnerCurrent || owner.OwnerObservationRevision != firstSurface.ListenerOwner.ObservationRevision || owner.Socket.IPv6Only == nil || *owner.Socket.IPv6Only || !slices.Equal(owner.Socket.CoverageFamilies, []hostsurface.Family{hostsurface.FamilyIPv4, hostsurface.FamilyIPv6}) {
		t.Fatalf("MANAGED_EXACT owner observation was weakened: %#v", owner)
	}
	if owner.Process == nil || owner.Service == nil || owner.Application == nil || owner.DeploymentBindingRevision == "" || owner.Application.DeploymentID != first.Capabilities.ExpectedListenerOwner.DeploymentID {
		t.Fatalf("bounded process/service/deployment proof is incomplete: %#v", owner)
	}
	if evidence.GraphRevision != graph.Revision || evidence.OwnerObservationRevision != graph.OwnerObservationRevision || evidence.Revision == "" {
		t.Fatalf("graph evidence revision binding is incomplete: %#v", evidence)
	}
}

func TestSocketGraphEvidencePreservesBlockingClassificationsAndReasons(t *testing.T) {
	now := time.Unix(9_000, 0).UTC()
	tests := []struct {
		name                      string
		hostSurfaceClassification hostsurface.Classification
		classification            hostsurface.Classification
		reason                    string
	}{
		{"unknown", hostsurface.ClassificationUnknownOwner, hostsurface.ClassificationUnknownOwner, "listener_owner_unknown"},
		{"expected_managed", hostsurface.ClassificationExpectedManaged, hostsurface.ClassificationUnknownOwner, "listener_owner_unknown"},
		{"unobserved", hostsurface.ClassificationUnobserved, hostsurface.ClassificationUnobserved, "listener_unobserved"},
		{"foreign", hostsurface.ClassificationForeign, hostsurface.ClassificationForeign, "listener_owner_foreign"},
		{"stale", hostsurface.ClassificationStale, hostsurface.ClassificationStale, "listener_owner_stale"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource := graphResource("fixture:evidence:"+test.name, hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "192.0.2.50", 443)
			surface := graphSurface(resource, now)
			surface.Classification = test.hostSurfaceClassification
			if test.hostSurfaceClassification != hostsurface.ClassificationStale {
				surface.ListenerOwner = nil
			}
			graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{surface}, Now: now})
			evidence, err := BuildSocketOwnershipGraphEvidence(graph, []hostresources.ProtectableResource{resource}, []hostsurface.HostSurfaceFactV1{surface}, now)
			if err != nil {
				t.Fatal(err)
			}
			node := evidence.Nodes[0]
			if !node.ApplyBlocked || !slices.Contains(node.ReasonCodes, test.reason) || node.OwnerObservations[0].Classification != test.classification || node.OwnerObservations[0].HostSurfaceClassification != test.hostSurfaceClassification {
				t.Fatalf("blocking classification or typed reason was lost: %#v", node)
			}
		})
	}
	resource := graphResource("fixture:evidence:absent", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "192.0.2.51", 443)
	graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Now: now})
	evidence, err := BuildSocketOwnershipGraphEvidence(graph, []hostresources.ProtectableResource{resource}, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Nodes[0].OwnerObservations[0].EvidenceSource != "no_host_surface" || evidence.Nodes[0].OwnerObservations[0].Classification != hostsurface.ClassificationUnobserved {
		t.Fatalf("absent HostSurface was not represented explicitly: %#v", evidence.Nodes[0].OwnerObservations)
	}
}

func TestSocketGraphEvidenceOrderingBindingAndBoundsFailClosed(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	resource := graphResource("fixture:evidence:bounds", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "192.0.2.60", 443)
	surface := graphSurface(resource, now)
	surface.ReasonCodes = []string{"z_reason", "a_reason", "z_reason"}
	graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{surface}, Now: now})
	evidence, err := BuildSocketOwnershipGraphEvidence(graph, []hostresources.ProtectableResource{resource}, []hostsurface.HostSurfaceFactV1{surface}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.IsSorted(evidence.Nodes[0].ReasonCodes) || !slices.IsSorted(evidence.Nodes[0].ObservedClaims[0].ReasonCodes) {
		t.Fatal("graph evidence reason codes are not deterministic")
	}
	again, err := BuildSocketOwnershipGraphEvidence(graph, []hostresources.ProtectableResource{resource}, []hostsurface.HostSurfaceFactV1{surface}, now.Add(time.Second))
	if err != nil || evidence.Revision != again.Revision {
		t.Fatalf("semantic graph evidence revision changed with capture time: %v", err)
	}
	stale := evidence
	stale.GraphRevision = strings.Repeat("f", 64)
	stale.Revision = socketGraphEvidenceRevision(stale)
	if ValidateSocketOwnershipGraphEvidence(stale, graph) == nil {
		t.Fatal("stale evidence was accepted for a newer/different graph revision")
	}
	missing := evidence
	missing.Nodes = nil
	missing.Revision = socketGraphEvidenceRevision(missing)
	if ValidateSocketOwnershipGraphEvidence(missing, graph) == nil {
		t.Fatal("missing graph node details satisfied the evidence contract")
	}
	mixedClaim := evidence
	mixedClaim.Nodes = append([]SocketGraphNodeEvidenceV1(nil), evidence.Nodes...)
	mixedClaim.Nodes[0].DesiredClaims = append([]SocketClaimEvidenceV1(nil), evidence.Nodes[0].DesiredClaims...)
	mixedClaim.Nodes[0].DesiredClaims[0].Key.Port++
	mixedClaim.Revision = socketGraphEvidenceRevision(mixedClaim)
	if ValidateSocketOwnershipGraphEvidence(mixedClaim, graph) == nil {
		t.Fatal("claim evidence from another snapshot satisfied the frozen graph contract")
	}
	mixedSource := evidence
	mixedSource.Nodes = append([]SocketGraphNodeEvidenceV1(nil), evidence.Nodes...)
	mixedSource.Nodes[0].DesiredClaims = append([]SocketClaimEvidenceV1(nil), evidence.Nodes[0].DesiredClaims...)
	mixedSource.Nodes[0].DesiredClaims[0].SourceID = "endpoint:from-another-snapshot"
	mixedSource.Revision = socketGraphEvidenceRevision(mixedSource)
	if ValidateSocketOwnershipGraphEvidence(mixedSource, graph) == nil {
		t.Fatal("claim source from another snapshot satisfied the frozen evidence contract")
	}
	mixedOwner := evidence
	mixedOwner.Nodes = append([]SocketGraphNodeEvidenceV1(nil), evidence.Nodes...)
	mixedOwner.Nodes[0].OwnerObservations = append([]SocketOwnerObservationEvidenceV1(nil), evidence.Nodes[0].OwnerObservations...)
	mixedOwner.Nodes[0].OwnerObservations[0].OwnerObservationRevision = strings.Repeat("e", 64)
	mixedOwner.Revision = socketGraphEvidenceRevision(mixedOwner)
	if ValidateSocketOwnershipGraphEvidence(mixedOwner, graph) == nil {
		t.Fatal("newer owner observation with the same SurfaceID satisfied the frozen claim contract")
	}
	noncurrentManaged := evidence
	noncurrentManaged.Nodes = append([]SocketGraphNodeEvidenceV1(nil), evidence.Nodes...)
	noncurrentManaged.Nodes[0].OwnerObservations = append([]SocketOwnerObservationEvidenceV1(nil), evidence.Nodes[0].OwnerObservations...)
	noncurrentManaged.Nodes[0].OwnerObservations[0].ListenerOwnerCurrent = false
	noncurrentManaged.Revision = socketGraphEvidenceRevision(noncurrentManaged)
	if ValidateSocketOwnershipGraphEvidence(noncurrentManaged, graph) == nil {
		t.Fatal("non-current MANAGED_EXACT owner fact satisfied the evidence contract")
	}
	mixedClassification := evidence
	mixedClassification.Nodes = append([]SocketGraphNodeEvidenceV1(nil), evidence.Nodes...)
	mixedClassification.Nodes[0].OwnerObservations = append([]SocketOwnerObservationEvidenceV1(nil), evidence.Nodes[0].OwnerObservations...)
	mixedClassification.Nodes[0].OwnerObservations[0].HostSurfaceClassification = hostsurface.ClassificationForeign
	mixedClassification.Revision = socketGraphEvidenceRevision(mixedClassification)
	if ValidateSocketOwnershipGraphEvidence(mixedClassification, graph) == nil {
		t.Fatal("effective owner classification diverged from the frozen HostSurface classification")
	}
	overflow := graph
	overflow.Nodes = append([]SocketGraphNode(nil), graph.Nodes...)
	overflow.Nodes[0].ReasonCodes = make([]string, MaxSocketGraphEvidenceReasons+1)
	for index := range overflow.Nodes[0].ReasonCodes {
		overflow.Nodes[0].ReasonCodes[index] = fmt.Sprintf("reason_%02d", index)
	}
	overflow.Revision = graphRevision(overflow)
	if _, err := BuildSocketOwnershipGraphEvidence(overflow, []hostresources.ProtectableResource{resource}, []hostsurface.HostSurfaceFactV1{surface}, now); err == nil {
		t.Fatal("unbounded node reason codes satisfied the evidence contract")
	}
}

func TestSocketGraphEvidenceOmitsRawPathsSecretsAndEnvironment(t *testing.T) {
	now := time.Unix(11_000, 0).UTC()
	resource := graphResource("fixture:evidence:redaction", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "192.0.2.70", 443)
	surface := graphSurface(resource, now)
	graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{surface}, Now: now})
	evidence, err := BuildSocketOwnershipGraphEvidence(graph, []hostresources.ProtectableResource{resource}, []hostsurface.HostSurfaceFactV1{surface}, now)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(evidence)
	text := string(encoded)
	for _, forbidden := range []string{"/usr/local/bin/solovey-ui", "/etc/systemd/system/solovey-ui.service", "/system.slice/solovey-ui.service", "processEnvironment", "password", "csrfToken", "privateKey"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("graph evidence leaked forbidden raw diagnostic %q", forbidden)
		}
	}
	for _, required := range []string{"executablePathSha256", "fragmentPathSha256", "controlGroupSha256", "deploymentBindingRevision"} {
		if !strings.Contains(text, required) {
			t.Fatalf("graph evidence omitted bounded identity digest %q", required)
		}
	}
}

func TestSocketGraphEvidencePreservesUnregisteredForeignCollisionSurface(t *testing.T) {
	now := time.Unix(12_000, 0).UTC()
	resource := graphResource("fixture:evidence:collision", hostresources.NetworkTCP, hostresources.AddressFamilyIPv4, "0.0.0.0", 443)
	managed := graphSurface(resource, now)
	foreign := hostsurface.HostSurfaceFactV1{Schema: hostsurface.SchemaV1, ID: "surface:foreign:443", Network: hostsurface.NetworkTCP, Family: hostsurface.FamilyIPv4, Bind: "192.0.2.80", Port: 443, SocketInode: "900", SocketCookie: 901, Classification: hostsurface.ClassificationUnexpectedPublic}
	graph := BuildSocketOwnershipGraph(SocketGraphInput{Resources: []hostresources.ProtectableResource{resource}, Surfaces: []hostsurface.HostSurfaceFactV1{managed, foreign}, Now: now})
	evidence, err := BuildSocketOwnershipGraphEvidence(graph, []hostresources.ProtectableResource{resource}, []hostsurface.HostSurfaceFactV1{managed, foreign}, now)
	if err != nil {
		t.Fatal(err)
	}
	node := evidence.Nodes[0]
	if !node.ApplyBlocked || !slices.Contains(node.ReasonCodes, "foreign_socket_collision") || len(node.OwnerObservations) != 2 {
		t.Fatalf("foreign collision evidence was lost: %#v", node)
	}
	var found bool
	for _, observation := range node.OwnerObservations {
		if observation.SurfaceID == foreign.ID {
			found = observation.RegisteredResourceID == "" && observation.Classification == hostsurface.ClassificationUnknownOwner && observation.HostSurfaceClassification == hostsurface.ClassificationUnexpectedPublic && observation.Socket.Inode == foreign.SocketInode && observation.Socket.Cookie == foreign.SocketCookie
		}
	}
	if !found {
		t.Fatalf("unregistered foreign socket identity was not retained: %#v", node.OwnerObservations)
	}
}
