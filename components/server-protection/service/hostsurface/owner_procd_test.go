package hostsurface

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/deploymentidentity"
	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
)

func TestHelperOwnerObserverProjectsProcdExpectation(t *testing.T) {
	now := time.Unix(7_000, 0).UTC()
	resource := procdHostResource(t)
	fact := procdHostOwnerFact(resource, now)
	metadata := protectionhelper.ExecutionMetadata{
		HelperIdentityRevision: strings.Repeat("9", 64), CapabilityRevision: strings.Repeat("a", 64),
		ListenerOwnerContractRevision: resource.Capabilities.ExpectedApplicationOwner.ContractRevision,
		ListenerOwnerObserverRevision: strings.Repeat("b", 64),
	}
	observer := HelperOwnerObserver{Now: func() time.Time { return now }, Helper: ownerExecutorFunc(func(_ context.Context, request protectionhelper.Request) (protectionhelper.Response, protectionhelper.ExecutionMetadata, error) {
		owner := request.ListenerOwnerObserve
		if owner == nil || owner.ExpectedOwnerContractRevision != resource.Capabilities.ExpectedApplicationOwner.ContractRevision {
			t.Fatalf("semantic owner expectation was not projected into the typed request: %#v", owner)
		}
		return testOwnerResponse(testOwnerResult([]hostfacts.ListenerOwnerFactV1{fact})), metadata, nil
	})}
	outcome := observer.ObserveOwner(context.Background(), resource)
	if outcome.Availability != OwnerObservationSuccess || outcome.Observation == nil {
		t.Fatalf("procd owner observation failed: %#v", outcome)
	}
	raw := RawSocket{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: resource.Listen, Port: uint16(resource.Port), Inode: fact.Socket.Inode}
	if matched := matchingOwnerFacts(raw, outcome.Observation, resource, now); len(matched) != 1 {
		t.Fatalf("procd owner fact did not match the resource: %#v", matched)
	}

	drift := *outcome.Observation
	drift.Facts = append([]hostfacts.ListenerOwnerFactV1(nil), outcome.Observation.Facts...)
	drift.Facts[0].Application.DeploymentID = "dep-" + strings.Repeat("9", 64)
	drift.Facts[0].Seal()
	testSealOwnerResult(&drift)
	if matched := matchingOwnerFacts(raw, &drift, resource, now); len(matched) != 0 {
		t.Fatalf("semantic deployment drift matched the resource: %#v", matched)
	}
}

func TestListenerOwnerExpectationRejectsInvalidSemanticContract(t *testing.T) {
	resource := procdHostResource(t)
	resource.Capabilities.ExpectedApplicationOwner.ContractRevision = "invalid"
	if _, ok := listenerOwnerExpectationFor(resource); ok {
		t.Fatal("resource with an invalid expected application owner was accepted")
	}
}

func procdHostResource(t *testing.T) hostresources.ProtectableResource {
	t.Helper()
	contract, err := deploymentidentity.NewProcdV1(
		"00112233-4455-4677-8899-aabbccddeeff", "src-"+strings.Repeat("1", 64), "art-"+strings.Repeat("2", 64), "dep-"+strings.Repeat("3", 64),
		strings.Repeat("4", 64), strings.Repeat("5", 64), "solovey-ui-panel", "solovey-ui", "panel",
		"/usr/lib/solovey-ui/solovey-ui", strings.Repeat("6", 64), 997, 997,
	)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := deploymentidentity.ExpectedProcdApplicationOwner(contract)
	if err != nil {
		t.Fatal(err)
	}
	resource := hostresources.ProtectableResource{
		ID: "core:panel:web", Kind: "panel_web", Owner: "core", Protocol: "tcp", Listen: "192.0.2.10", Port: 443, Public: true,
		Capabilities: hostresources.ProtectableResourceCapabilities{
			Known: true, OwnerRevision: strings.Repeat("7", 64), ConfigRevision: strings.Repeat("8", 64),
			ExpectedApplicationOwner: expected,
		},
	}
	resource.ListenIntent = hostresources.BuildConfiguredListenIntent(resource)
	return resource
}

func procdHostOwnerFact(resource hostresources.ProtectableResource, now time.Time) hostfacts.ListenerOwnerFactV1 {
	expected := resource.Capabilities.ExpectedApplicationOwner
	pid, parent, session, uid, gid := 100, 1, 100, int(expected.ProcessUID), int(expected.ProcessGID)
	fact := hostfacts.ListenerOwnerFactV1{
		Schema: hostfacts.ListenerOwnerFactSchemaV1,
		Socket: hostfacts.ListenerSocketIdentityV1{
			Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: resource.Listen, Port: uint16(resource.Port),
			Inode: "200", Cookie: 300, CoverageFamilies: []hostfacts.Family{hostfacts.FamilyIPv4},
		},
		Process: hostfacts.ProcessFact{
			ProviderRevision: "fixture-process-evidence/v1", EvidenceRevision: strings.Repeat("b", 64),
			PID: &pid, ParentPID: &parent, SessionID: &session, StartTime: "400", ExeDigest: expected.ExecutableSHA256,
			Executable: expected.ExecutablePath, ExeDevice: 10, ExeInode: 20, UID: &uid, GID: &gid,
		},
		Service: hostfacts.ServiceFact{
			SupervisorRevision: strings.Repeat("9", 64), CgroupAvailability: "unavailable", CgroupPolicy: "optional", CgroupRevision: strings.Repeat("a", 64),
			MainPID: &pid, ActiveState: "active", SubState: "running", ProcdService: "solovey-ui", ProcdInstance: "panel",
			ProcdCommand: []string{"/usr/lib/solovey-ui/solovey-broker-readiness"}, ProcdUser: "solovey-ui", ProcdGroup: "solovey-ui",
		},
		Application: hostfacts.ListenerApplicationIdentityV1{
			InstanceID: expected.InstanceID, SourceRevision: expected.SourceRevision, ArtifactRevision: expected.ArtifactRevision,
			DeploymentID: expected.DeploymentID, OwnerContractRevision: expected.ContractRevision,
			RuntimeRootBindingRevision: expected.RuntimeRootBindingRevision, ExpectedExecutableSHA256: expected.ExecutableSHA256,
			ServiceIdentity: expected.ServiceIdentity, ResourceID: resource.ID,
			ResourceOwnerRevision: resource.Capabilities.OwnerRevision, ConfigurationRevision: resource.Capabilities.ConfigRevision,
		},
		ObservedAt: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(),
	}
	fact.Seal()
	return fact
}
