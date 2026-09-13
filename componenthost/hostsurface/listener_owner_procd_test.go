package hostsurface

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestProcdListenerOwnerFactBindsSupervisorAndMainProcess(t *testing.T) {
	now := time.Unix(7_000, 0).UTC()
	fact := procdOwnerFactFixture(now)
	if !fact.Valid(now) {
		t.Fatalf("valid procd listener owner fact was rejected: %#v", fact)
	}

	mutations := []func(*ListenerOwnerFactV1){
		func(value *ListenerOwnerFactV1) { value.Process.ProviderRevision = "" },
		func(value *ListenerOwnerFactV1) { value.Process.EvidenceRevision = "" },
		func(value *ListenerOwnerFactV1) { value.Service.SupervisorRevision = "" },
		func(value *ListenerOwnerFactV1) { value.Service.CgroupRevision = "" },
		func(value *ListenerOwnerFactV1) { value.Service.ProcdService = "" },
		func(value *ListenerOwnerFactV1) { value.Service.ProcdInstance = "" },
		func(value *ListenerOwnerFactV1) { value.Service.ProcdCommand = []string{"relative"} },
		func(value *ListenerOwnerFactV1) { (*value.Service.MainPID)++ },
		func(value *ListenerOwnerFactV1) { value.Service.SystemdUnit = "solovey-ui.service" },
	}
	for index, mutate := range mutations {
		candidate := fact
		candidate.Service.ProcdCommand = append([]string(nil), fact.Service.ProcdCommand...)
		pid := *fact.Service.MainPID
		candidate.Service.MainPID = &pid
		mutate(&candidate)
		candidate.Seal()
		if candidate.Valid(now) {
			t.Fatalf("procd listener owner drift %d was accepted: %#v", index, candidate)
		}
	}
}

func TestSystemdServiceFactSerializationDoesNotGainProcdFields(t *testing.T) {
	pid := 100
	data, err := json.Marshal(ServiceFact{
		SystemdUnit: "solovey-ui.service", MainPID: &pid, FragmentPath: "/etc/systemd/system/solovey-ui.service",
		FragmentSHA256: strings.Repeat("1", 64), ActiveState: "active", SubState: "running",
		ControlGroup: "/system.slice/solovey-ui.service", StartMonotonicUsec: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"procdService", "procdInstance", "procdCommand", "procdUser", "procdGroup"} {
		if strings.Contains(string(data), field) {
			t.Fatalf("systemd v1 serialization gained %q: %s", field, data)
		}
	}
}

func procdOwnerFactFixture(now time.Time) ListenerOwnerFactV1 {
	pid, parent, session, uid, gid := 100, 1, 100, 997, 997
	fact := ListenerOwnerFactV1{
		Schema: ListenerOwnerFactSchemaV1,
		Socket: ListenerSocketIdentityV1{
			Network: NetworkTCP, Family: FamilyIPv4, Bind: "192.0.2.10", Port: 443,
			Inode: "200", Cookie: 300, CoverageFamilies: []Family{FamilyIPv4},
		},
		Process: ProcessFact{
			ProviderRevision: "fixture-process-evidence/v1", EvidenceRevision: strings.Repeat("b", 64),
			PID: &pid, ParentPID: &parent, SessionID: &session, StartTime: "400", ExeDigest: strings.Repeat("6", 64),
			Executable: "/usr/lib/solovey-ui/solovey-ui", ExeDevice: 10, ExeInode: 20, UID: &uid, GID: &gid,
		},
		Service: ServiceFact{
			SupervisorRevision: strings.Repeat("9", 64), CgroupAvailability: "unavailable", CgroupPolicy: "optional", CgroupRevision: strings.Repeat("a", 64),
			MainPID: &pid, ActiveState: "active", SubState: "running", ProcdService: "solovey-ui", ProcdInstance: "panel",
			ProcdCommand: []string{"/usr/lib/solovey-ui/solovey-broker-readiness"}, ProcdUser: "solovey-ui", ProcdGroup: "solovey-ui",
		},
		Application: ListenerApplicationIdentityV1{
			InstanceID: "00112233-4455-4677-8899-aabbccddeeff", SourceRevision: "src-" + strings.Repeat("1", 64),
			ArtifactRevision: "art-" + strings.Repeat("2", 64), DeploymentID: "dep-" + strings.Repeat("3", 64),
			OwnerContractRevision: strings.Repeat("4", 64), RuntimeRootBindingRevision: strings.Repeat("5", 64),
			ExpectedExecutableSHA256: strings.Repeat("6", 64), ServiceIdentity: "solovey-ui-panel", ResourceID: "core:panel:web",
			ResourceOwnerRevision: strings.Repeat("7", 64), ConfigurationRevision: strings.Repeat("8", 64),
		},
		ObservedAt: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(),
	}
	fact.Seal()
	return fact
}
