package sshmanagement

import (
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
)

func TestProtectableResourcesComeFromExactListenerAuthority(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	posture := validPostureFixture(now)
	pid, parent, session, root := 784, 1, 784, 0
	authority := SSHListenerAuthorityV1{
		Schema: ListenerAuthoritySchemaV1, EndpointIDs: []string{posture.Endpoints[0].ID}, InstanceID: "instance1",
		Socket: hostfacts.ListenerSocketIdentityV1{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "192.0.2.5", Port: 22,
			Inode: "22", Cookie: 42, CoverageFamilies: []hostfacts.Family{hostfacts.FamilyIPv4}},
		Process: hostfacts.ProcessFact{ProviderRevision: "process-evidence/v2", EvidenceRevision: Revision("process"), PID: &pid, ParentPID: &parent,
			SessionID: &session, StartTime: "123", ExeDigest: posture.BinaryRevision, Executable: "/usr/sbin/dropbear", ExeDevice: 8, ExeInode: 42, UID: &root, GID: &root},
		Service: hostfacts.ServiceFact{SupervisorRevision: Revision("supervisor"), CgroupAvailability: "unavailable", CgroupPolicy: "optional",
			CgroupRevision: Revision("cgroup"), MainPID: &pid, ActiveState: "active", SubState: "running", ProcdService: "dropbear",
			ProcdInstance: "instance1", ProcdCommand: []string{"/usr/sbin/dropbear", "-F", "-P", "/var/run/dropbear.main.pid", "-p", "22"}},
		BinaryRevision: posture.BinaryRevision, ServiceRevision: posture.ServiceRevision, ConfigurationRevision: posture.ConfigurationRevision,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(),
	}
	authority.Seal()
	posture.ListenerAuthorities = []SSHListenerAuthorityV1{authority}
	posture.SemanticRevision = PostureSemanticRevision(posture)
	resources, err := ProtectableResources(posture, now)
	if err != nil || len(resources) != 1 {
		t.Fatalf("resources=%#v err=%v", resources, err)
	}
	resource := resources[0]
	if resource.ID != ProtectionResourceID(authority) || resource.Kind != ProtectionResourceKind || resource.Owner != ProtectionResourceOwner ||
		resource.Port != 22 || resource.Listen != "192.0.2.5" || resource.Capabilities.OwnerRevision != posture.SemanticRevision ||
		resource.Capabilities.ConfigRevision != posture.ConfigurationRevision || len(resource.Endpoints) != 1 || resource.Endpoints[0].ID == "" || !resource.Endpoints[0].Known() {
		t.Fatalf("exact SSH resource projection was incomplete: %#v", resource)
	}

	changed := posture
	changed.ListenerAuthorities = append([]SSHListenerAuthorityV1(nil), posture.ListenerAuthorities...)
	changed.ListenerAuthorities[0].EndpointIDs = []string{"management:ssh:other"}
	changed.ListenerAuthorities[0].Seal()
	changed.SemanticRevision = PostureSemanticRevision(changed)
	if _, err := ProtectableResources(changed, now); err == nil {
		t.Fatal("authority detached from the SSH endpoint was accepted")
	}
}
