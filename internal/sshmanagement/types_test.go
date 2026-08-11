package sshmanagement

import (
	"strings"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

func TestManagedPolicyRendersOnlySixBoundedFields(t *testing.T) {
	tries := uint16(4)
	grace := uint32(45)
	disabled, enabled := false, true
	policy := DesiredPolicyV1{Schema: PolicySchemaV1, MaxAuthTries: &tries, LoginGraceTimeSeconds: &grace,
		PasswordAuthentication: &disabled, KbdInteractiveAuthentication: &disabled,
		PermitRootLogin: RootLoginProhibitPassword, PubkeyAuthentication: &enabled}
	content, err := policy.RenderManagedDropIn()
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, expected := range []string{"MaxAuthTries 4", "LoginGraceTime 45", "PasswordAuthentication no", "KbdInteractiveAuthentication no", "PermitRootLogin prohibit-password", "PubkeyAuthentication yes"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("managed content omitted %q: %s", expected, text)
		}
	}
	for _, forbidden := range []string{"Match ", "Include ", "AllowUsers", "DenyUsers", "AuthorizedKeys", "AuthenticationMethods", "Port ", "ListenAddress", "Subsystem", "Banner", "PAM"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("managed content contains forbidden directive %q", forbidden)
		}
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
	posture.SemanticRevision = PostureSemanticRevision(posture)
	return posture
}

func hasReason(values []ReasonCode, expected ReasonCode) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func BenchmarkRenderManagedDropIn(b *testing.B) {
	tries := uint16(4)
	grace := uint32(30)
	password, keyboard, publicKey := false, false, true
	policy := DesiredPolicyV1{Schema: PolicySchemaV1, MaxAuthTries: &tries, LoginGraceTimeSeconds: &grace,
		PasswordAuthentication: &password, KbdInteractiveAuthentication: &keyboard,
		PermitRootLogin: RootLoginProhibitPassword, PubkeyAuthentication: &publicKey}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if _, err := policy.RenderManagedDropIn(); err != nil {
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
