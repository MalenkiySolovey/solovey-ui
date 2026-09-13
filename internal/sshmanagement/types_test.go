package sshmanagement

import (
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func TestManagedPolicyValidatesOnlySixSemanticFields(t *testing.T) {
	tries := uint16(4)
	grace := uint32(45)
	disabled, enabled := false, true
	policy := DesiredPolicyV1{Schema: PolicySchemaV1, MaxAuthTries: &tries, LoginGraceTimeSeconds: &grace,
		PasswordAuthentication: &disabled, KbdInteractiveAuthentication: &disabled,
		PermitRootLogin: RootLoginProhibitPassword, PubkeyAuthentication: &enabled}
	if err := policy.Validate(); err != nil {
		t.Fatalf("valid semantic policy rejected: %v", err)
	}
}

func TestPolicyBoundsFailClosed(t *testing.T) {
	zero := uint16(0)
	tooLong := uint32(601)
	for _, policy := range []DesiredPolicyV1{
		{Schema: PolicySchemaV1, PermitRootLogin: RootLoginUnchanged, MaxAuthTries: &zero},
		{Schema: PolicySchemaV1, PermitRootLogin: RootLoginUnchanged, LoginGraceTimeSeconds: &tooLong},
		{Schema: PolicySchemaV1, PermitRootLogin: RootLoginPolicy("forced-commands-only")},
		{Schema: "future", PermitRootLogin: RootLoginUnchanged},
	} {
		if err := policy.Validate(); err == nil {
			t.Fatalf("unsafe policy accepted: %#v", policy)
		}
	}
}

func TestPostureRevisionExcludesObservationTimeButBindsSemantics(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	posture := validPostureFixture(now)
	first := PostureSemanticRevision(posture)
	posture.ObservedAt += 10
	posture.ExpiresAt += 10
	for index := range posture.Endpoints {
		posture.Endpoints[index].ObservedAt += 10
		posture.Endpoints[index].ExpiresAt += 10
	}
	if second := PostureSemanticRevision(posture); second != first {
		t.Fatalf("observation-only change altered semantic revision: %s != %s", first, second)
	}
	posture.Authentication.MaxAuthTries = 7
	if second := PostureSemanticRevision(posture); second == first {
		t.Fatal("semantic authentication change did not alter revision")
	}
}

func TestPostureRejectsUnknownMatchUnsafeFilesAndSymlinks(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	for _, mutate := range []func(*SSHPostureV1){
		func(value *SSHPostureV1) { value.MatchContexts[0].Known = false },
		func(value *SSHPostureV1) { value.ConfigGraph[0].Owner = "user" },
		func(value *SSHPostureV1) { value.ConfigGraph[0].Symlink = true },
		func(value *SSHPostureV1) { value.HostKeys[0].ModeClass = "world_readable" },
		func(value *SSHPostureV1) { value.Authentication.PasswordAuthentication = "maybe" },
		func(value *SSHPostureV1) { value.Forwarding.AllowTCPForwarding = "caller-controlled" },
		func(value *SSHPostureV1) {
			value.ConfigGraph = append(value.ConfigGraph, ConfigNodeV1{ID: "cycle", ParentID: "cycle", Kind: "include", Order: 1, Depth: 1, Digest: Revision("cycle"), Owner: "root", ModeClass: "owner_read"})
		},
	} {
		posture := validPostureFixture(now)
		mutate(&posture)
		posture.SemanticRevision = PostureSemanticRevision(posture)
		if err := posture.Validate(now); err == nil {
			t.Fatalf("unsafe posture accepted: %#v", posture)
		}
	}
}

func TestSSHListenerAuthorityBindsExactFreshRootEvidence(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	pid, parent, session, root := 784, 1, 784, 0
	authority := SSHListenerAuthorityV1{
		Schema: ListenerAuthoritySchemaV1, EndpointIDs: []string{"management:ssh:configured:ipv4:2222:lan:0123456789abcdef"}, InstanceID: "lan",
		Socket: hostfacts.ListenerSocketIdentityV1{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 2222,
			Inode: "22", Cookie: 42, Wildcard: true, CoverageFamilies: []hostfacts.Family{hostfacts.FamilyIPv4}},
		Process: hostfacts.ProcessFact{ProviderRevision: "process-evidence/v2", EvidenceRevision: Revision("process"), PID: &pid, ParentPID: &parent,
			SessionID: &session, StartTime: "123", ExeDigest: Revision("binary"), Executable: "/usr/sbin/dropbear", ExeDevice: 8, ExeInode: 42, UID: &root, GID: &root},
		Service: hostfacts.ServiceFact{SupervisorRevision: Revision("supervisor"), CgroupAvailability: "unavailable", CgroupPolicy: "optional",
			CgroupRevision: Revision("cgroup"), MainPID: &pid, ActiveState: "active", SubState: "running", ProcdService: "dropbear",
			ProcdInstance: "lan", ProcdCommand: []string{"/usr/sbin/dropbear", "-F", "-p", "2222"}},
		BinaryRevision: Revision("binary"), ServiceRevision: Revision("service"), ConfigurationRevision: Revision("configuration"),
		ObservedAt: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(),
	}
	authority.Seal()
	if !authority.Valid(now) {
		t.Fatalf("exact listener authority rejected: %#v", authority)
	}
	t.Run("supervisor-instance-mismatch", func(t *testing.T) {
		changed := authority
		changed.Service.ProcdInstance = "other"
		changed.Seal()
		if changed.Valid(now) {
			t.Fatal("authority was accepted for a different procd instance")
		}
	})

	for name, mutate := range map[string]func(*SSHListenerAuthorityV1){
		"socket-inode":  func(value *SSHListenerAuthorityV1) { value.Socket.Inode = "23" },
		"process-start": func(value *SSHListenerAuthorityV1) { value.Process.StartTime = "124" },
		"binary":        func(value *SSHListenerAuthorityV1) { value.Process.ExeDigest = Revision("forged") },
		"duplicate-id": func(value *SSHListenerAuthorityV1) {
			value.EndpointIDs = append(value.EndpointIDs, value.EndpointIDs[0])
			value.Seal()
		},
		"future": func(value *SSHListenerAuthorityV1) {
			value.ObservedAt = now.Add(time.Second).Unix()
			value.ExpiresAt = now.Add(31 * time.Second).Unix()
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := authority
			changed.EndpointIDs = append([]string(nil), authority.EndpointIDs...)
			mutate(&changed)
			if changed.Valid(now) {
				t.Fatal("changed authority was accepted")
			}
		})
	}
	if authority.Valid(now.Add(31 * time.Second)) {
		t.Fatal("expired listener authority was accepted")
	}
}

func TestPreservationRejectsLastPathAndPasswordDisableWithoutTwoProofs(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	posture := validPostureFixture(now)
	disabled := false
	plan := BuildPreservationPlan(PreservationInput{Before: posture.Endpoints, After: posture.Endpoints, Now: now,
		Policy: DesiredPolicyV1{Schema: PolicySchemaV1, PermitRootLogin: RootLoginUnchanged, PasswordAuthentication: &disabled}, Watchdog: true})
	if plan.Safe || !hasReason(plan.ReasonCodes, ReasonRecoveryPathMissing) || !hasReason(plan.ReasonCodes, ReasonConsoleMissing) || !hasReason(plan.ReasonCodes, ReasonFreshPubkeyMissing) {
		t.Fatalf("unsafe password disable plan=%#v", plan)
	}
	plan = BuildPreservationPlan(PreservationInput{Before: posture.Endpoints, After: nil, Now: now,
		Policy: DesiredPolicyV1{Schema: PolicySchemaV1, PermitRootLogin: RootLoginUnchanged}, Watchdog: true})
	if plan.Safe || !hasReason(plan.ReasonCodes, ReasonManagementPathRemoved) {
		t.Fatalf("last path removal plan=%#v", plan)
	}
}

func validPostureFixture(now time.Time) SSHPostureV1 {
	configuration := Revision("config")
	capabilities := CapabilitySetV1{ObservePosture: AvailabilityAvailable, Prepare: AvailabilityAvailable, Stage: AvailabilityAvailable,
		Validate: AvailabilityAvailable, Reload: AvailabilityAvailable, Reconnect: AvailabilityAvailable, Rollback: AvailabilityAvailable}
	capabilities.Revision = Revision(capabilities)
	endpoint := hostresources.ManagementEndpointV1{Schema: hostresources.ManagementEndpointSchemaV1, ID: "management:ssh:one", Network: hostresources.NetworkTCP,
		Family: hostresources.AddressFamilyIPv4, Bind: "192.0.2.5", Port: 22, ServiceKind: hostresources.ManagementSSH,
		Exposure: hostresources.EndpointIntentPublic, Owner: "system", Purpose: "ssh_administrative_access", RecoveryPolicy: "fresh_independent_path_required",
		Source: "fixture", ObservedListener: true, ConfidenceBP: 10000, ObservedAt: now.Unix(), ExpiresAt: now.Add(5 * time.Minute).Unix(),
		ConfigurationRevision: configuration, SemanticRevision: configuration}
	posture := SSHPostureV1{Schema: PostureSchemaV1,
		Binary:        BinaryIdentityV1{Implementation: "openssh", VersionClass: "portable_9", Digest: Revision("binary"), Selected: true},
		Service:       ServiceIdentityV1{Manager: "systemd", UnitID: "sshd.service", State: "active", Digest: Revision("service")},
		ConfigGraph:   []ConfigNodeV1{{ID: "main", Kind: "main", Order: 0, Depth: 0, Digest: configuration, Owner: "root", ModeClass: "owner_read_write"}},
		MatchContexts: []MatchContextV1{{ID: "global", ConditionClass: "global", EffectiveHash: Revision("effective"), Known: true}},
		Endpoints:     []hostresources.ManagementEndpointV1{endpoint}, Authentication: AuthenticationPostureV1{PasswordAuthentication: "yes", KbdInteractiveAuthentication: "yes",
			PermitRootLogin: "prohibit-password", PubkeyAuthentication: "yes", AuthenticationMethods: []string{"publickey"}, MaxAuthTries: 6,
			LoginGraceTimeSeconds: 120, MaxStartupsClass: "bounded_default"},
		Forwarding:     ForwardingPostureV1{AllowAgentForwarding: "yes", AllowTCPForwarding: "yes", GatewayPorts: "no", PermitTunnel: "no", X11Forwarding: "yes"},
		AuthorizedKeys: AuthorizedKeysPostureV1{StrictModes: "yes", PathTemplateCount: 1, PathTemplateRevision: Revision("authorized-key-templates")},
		HostKeys:       []HostKeyPostureV1{{Type: "ed25519", Fingerprint: Revision("host-key"), Count: 1, Owner: "root", ModeClass: "owner_read"}},
		Capabilities:   capabilities,
		ObservedAt:     now.Unix(), ExpiresAt: now.Add(5 * time.Minute).Unix(), BinaryRevision: Revision("binary"), ServiceRevision: Revision("service"), ConfigurationRevision: configuration}
	pid, parent, session, root := 784, 1, 784, 0
	authority := SSHListenerAuthorityV1{
		Schema: ListenerAuthoritySchemaV1, EndpointIDs: []string{endpoint.ID}, InstanceID: "sshd.service",
		Socket: hostfacts.ListenerSocketIdentityV1{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: endpoint.Bind, Port: endpoint.Port,
			Inode: "22", Cookie: 42, CoverageFamilies: []hostfacts.Family{hostfacts.FamilyIPv4}},
		Process: hostfacts.ProcessFact{ProviderRevision: "process-evidence/v2", EvidenceRevision: Revision("process"), PID: &pid, ParentPID: &parent,
			SessionID: &session, StartTime: "123", ExeDigest: Revision("binary"), Executable: "/usr/sbin/sshd", ExeDevice: 8, ExeInode: 42,
			UID: &root, GID: &root, ControlGroup: "/system.slice/sshd.service"},
		Service: hostfacts.ServiceFact{SupervisorRevision: Revision("supervisor"), CgroupAvailability: "available", CgroupPolicy: "required",
			CgroupRevision: Revision("cgroup"), MainPID: &pid, ActiveState: "active", SubState: "running", SystemdUnit: "sshd.service",
			FragmentPath: "/usr/lib/systemd/system/sshd.service", FragmentSHA256: Revision("fragment"), ControlGroup: "/system.slice/sshd.service", StartMonotonicUsec: 1},
		BinaryRevision: Revision("binary"), ServiceRevision: Revision("service"), ConfigurationRevision: configuration,
		ObservedAt: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(),
	}
	authority.Seal()
	posture.ListenerAuthorities = []SSHListenerAuthorityV1{authority}
	posture.SemanticRevision = PostureSemanticRevision(posture)
	return posture
}

func TestPostureValidationDoesNotEncodeImplementationSupervisorPairs(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	posture := validPostureFixture(now)
	posture.Service.Manager = "procd"
	posture.Service.UnitID = "sshd"
	posture.SemanticRevision = PostureSemanticRevision(posture)
	if err := posture.Validate(now); err != nil {
		t.Fatalf("OpenSSH/procd posture rejected as a domain impossibility: %v", err)
	}
	posture.Binary.Implementation = "dropbear"
	posture.Service.Manager = "systemd"
	posture.Service.UnitID = "dropbear"
	posture.ConfigGraph = []ConfigNodeV1{{ID: "uci:dropbear", Kind: "uci_package", Order: 0, Depth: 0,
		Digest: posture.ConfigurationRevision, Owner: "root", ModeClass: "owner_read_write"},
		{ID: "uci:main", ParentID: "uci:dropbear", Kind: "uci_section", Order: 1, Depth: 1,
			Digest: Revision("dropbear-main"), Owner: "root", ModeClass: "owner_read_write"}}
	posture.SemanticRevision = PostureSemanticRevision(posture)
	if err := posture.Validate(now); err != nil {
		t.Fatalf("Dropbear/systemd posture rejected as a domain impossibility: %v", err)
	}
}

func TestBackendDiagnosticConfigKindIsOpenEnded(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	posture := validPostureFixture(now)
	posture.ConfigGraph[0].Kind = "future_backend_root"
	posture.SemanticRevision = PostureSemanticRevision(posture)
	if err := posture.Validate(now); err != nil {
		t.Fatalf("backend-native diagnostic kind required a semantic-domain enum change: %v", err)
	}
}

func hasReason(values []ReasonCode, expected ReasonCode) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func BenchmarkDesiredPolicyValidation(b *testing.B) {
	tries := uint16(4)
	grace := uint32(30)
	password, keyboard, publicKey := false, false, true
	policy := DesiredPolicyV1{Schema: PolicySchemaV1, MaxAuthTries: &tries, LoginGraceTimeSeconds: &grace,
		PasswordAuthentication: &password, KbdInteractiveAuthentication: &keyboard,
		PermitRootLogin: RootLoginProhibitPassword, PubkeyAuthentication: &publicKey}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if err := policy.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPostureSemanticRevision(b *testing.B) {
	now := time.Unix(10_000, 0).UTC()
	posture := validPostureFixture(now)
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if PostureSemanticRevision(posture) == "" {
			b.Fatal("empty revision")
		}
	}
}

func BenchmarkManagementPreservationPlan(b *testing.B) {
	now := time.Unix(10_000, 0).UTC()
	posture := validPostureFixture(now)
	path := hostresources.RecoveryPathV1{Schema: hostresources.RecoveryPathSchemaV1, ID: "recovery:benchmark", Kind: string(hostresources.ManagementSSH),
		EndpointID: posture.Endpoints[0].ID, PrincipalID: "principal:benchmark", VerificationMethod: "fresh_ssh_login",
		EvidenceProvider: "benchmark", TargetOperation: "ssh-operation:benchmark", VerifiedAt: now.Unix(), ExpiresAt: now.Add(10 * time.Minute).Unix(),
		IndependenceClass: "independent_reconnect", VerificationState: "verified", OperationBound: true, SingleUse: true, Revision: 1,
		SourceRevision: Revision("source"), ConfigurationRevision: posture.ConfigurationRevision, ServiceRevision: posture.ServiceRevision,
		BinaryRevision: posture.BinaryRevision, ProducerRevision: Revision("producer")}
	input := PreservationInput{Before: posture.Endpoints, After: posture.Endpoints, Recovery: []hostresources.RecoveryPathV1{path}, Now: now,
		Policy: DesiredPolicyV1{Schema: PolicySchemaV1, PermitRootLogin: RootLoginUnchanged}, Watchdog: true}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if plan := BuildPreservationPlan(input); !plan.Safe {
			b.Fatal("benchmark preservation plan became unsafe")
		}
	}
}
