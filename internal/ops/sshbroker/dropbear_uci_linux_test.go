//go:build linux

package sshbroker

import (
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
)

func TestDropbearUCISelectsNamedAndAnonymousSectionsWithoutFixedIndexAuthority(t *testing.T) {
	exported := []byte("package 'dropbear'\nconfig 'dropbear' 'lan'\n option 'Port' '22'\n option 'PasswordAuth' '1'\nconfig 'dropbear'\n option 'Port' '2222'\n list 'keyfile' '/etc/dropbear/a key'\nconfig 'dropbear' 'disabled'\n option 'Port' '2200'\n option 'enable' '0'\n")
	shown := []byte("dropbear.lan=dropbear\ndropbear.cfg02ab3c=dropbear\ndropbear.disabled=dropbear\n")
	sections, err := parseDropbearUCI(exported, shown)
	if err != nil || len(sections) != 3 || sections[0].Anonymous || !sections[1].Anonymous || sections[1].Internal != "cfg02ab3c" || !sections[1].Options["keyfile"].List {
		t.Fatalf("sections=%#v err=%v", sections, err)
	}
	config := dropbearConfig{Sections: sections, Revision: domain.Revision(sections)}
	named, err := selectDropbearSection(config, endpointID("ipv4", 22))
	if err != nil || named.Name != "lan" || dropbearSelector(named) != "lan" {
		t.Fatalf("named=%#v err=%v", named, err)
	}
	anonymous, err := selectDropbearSection(config, endpointID("ipv6", 2222))
	if err != nil || anonymous.Internal != "cfg02ab3c" || dropbearSelector(anonymous) != "@dropbear[1]" {
		t.Fatalf("anonymous=%#v err=%v", anonymous, err)
	}
	if _, err := selectDropbearSection(config, endpointID("ipv4", 2200)); err == nil {
		t.Fatal("disabled Dropbear section was selected")
	}
}

func TestDropbearTargetAmbiguityAndMalformedEvidenceFailClosed(t *testing.T) {
	sections, err := parseDropbearUCI(
		[]byte("package 'dropbear'\nconfig 'dropbear' 'one'\n option 'Port' '22'\nconfig 'dropbear' 'two'\n option 'Port' '22'\n"),
		[]byte("dropbear.one=dropbear\ndropbear.two=dropbear\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := selectDropbearSection(dropbearConfig{Sections: sections}, endpointID("ipv4", 22)); err == nil {
		t.Fatal("two enabled sections sharing an endpoint were accepted")
	}
	firstID := dropbearEndpointID(sections[0], hostresources.AddressFamilyIPv4, "0.0.0.0", 22)
	secondID := dropbearEndpointID(sections[1], hostresources.AddressFamilyIPv4, "127.0.0.1", 22)
	first, firstErr := selectDropbearSection(dropbearConfig{Sections: sections}, firstID)
	second, secondErr := selectDropbearSection(dropbearConfig{Sections: sections}, secondID)
	if firstErr != nil || secondErr != nil || first.Name != "one" || second.Name != "two" || firstID == secondID {
		t.Fatalf("instance-bound same-port identities were not exact: first=%#v second=%#v errors=%v/%v", first, second, firstErr, secondErr)
	}
	for _, fixture := range [][2][]byte{
		{[]byte("package 'dropbear'\nconfig 'dropbear'\n option 'Port' '22\n"), []byte("dropbear.cfg=dropbear\n")},
		{[]byte("package 'dropbear'\nconfig 'dropbear'\n"), []byte("dropbear.cfg=dropbear\ndropbear.extra=dropbear\n")},
		{[]byte(strings.Repeat("x", maxCommandOutput+1)), []byte("dropbear.cfg=dropbear\n")},
	} {
		if _, err := parseDropbearUCI(fixture[0], fixture[1]); err == nil {
			t.Fatal("malformed or oversized UCI evidence was accepted")
		}
	}
}

func TestDropbearProcdProjectionCorrelatesExactSectionAndConfiguredPort(t *testing.T) {
	raw := []byte(`{"dropbear":{"instances":{"instance1":{"running":true,"pid":301,"command":["/usr/sbin/dropbear","-F","-P","/var/run/dropbear.main.pid","-p","192.0.2.10:2222"]},"instance2":{"running":true,"pid":302,"command":["/usr/sbin/dropbear","-F","-P","/var/run/dropbear.other.pid","-p","[2001:db8::10]:2222"]},"instance3":{"running":false,"pid":303,"command":["/usr/sbin/dropbear","-F","-P","/var/run/dropbear.disabled.pid","-p","2222"]}}}}`)
	main, err := parseDropbearProcdInstance(raw, "main", 2222, "/usr/sbin/dropbear")
	if err != nil || main.Name != "instance1" || main.PID != 301 {
		t.Fatalf("non-default address-qualified listener was not correlated: instance=%#v err=%v", main, err)
	}
	other, err := parseDropbearProcdInstance(raw, "other", 2222, "/usr/sbin/dropbear")
	if err != nil || other.Name != "instance2" || other.PID != 302 {
		t.Fatalf("same-port second section was not correlated by its exact pidfile: instance=%#v err=%v", other, err)
	}
	for _, test := range []struct {
		name       string
		internal   string
		port       uint16
		executable string
	}{
		{name: "disabled", internal: "disabled", port: 2222, executable: "/usr/sbin/dropbear"},
		{name: "wrong-port", internal: "main", port: 22, executable: "/usr/sbin/dropbear"},
		{name: "fake-binary", internal: "main", port: 2222, executable: "/tmp/dropbear"},
		{name: "wrong-pidfile", internal: "missing", port: 2222, executable: "/usr/sbin/dropbear"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseDropbearProcdInstance(raw, test.internal, test.port, test.executable); err == nil {
				t.Fatal("uncorrelated procd instance was accepted")
			}
		})
	}
	ambiguous := []byte(`{"dropbear":{"instances":{"instance1":{"running":true,"pid":301,"command":["/usr/sbin/dropbear","-F","-P","/var/run/dropbear.main.pid","-p","2222"]},"instance2":{"running":true,"pid":302,"command":["/usr/sbin/dropbear","-F","-P","/var/run/dropbear.main.pid","-p","2222"]}}}}`)
	if _, err := parseDropbearProcdInstance(ambiguous, "main", 2222, "/usr/sbin/dropbear"); err == nil {
		t.Fatal("two opaque procd rows claiming one UCI section were accepted")
	}
}

func TestDropbearCommandBindsAddressQualifiedSocket(t *testing.T) {
	command := []string{"/usr/sbin/dropbear", "-F", "-p", "192.0.2.10:2222", "-p", "2001:db8::10:2222"}
	for _, socket := range []hostfacts.ListenerSocketIdentityV1{
		{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "192.0.2.10", Port: 2222},
		{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv6, Bind: "2001:db8::10", Port: 2222},
	} {
		if !dropbearCommandCoversSocket(command, socket) {
			t.Fatalf("configured address-qualified socket was rejected: %#v", socket)
		}
	}
	unrelated := hostfacts.ListenerSocketIdentityV1{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "192.0.2.11", Port: 2222}
	if dropbearCommandCoversSocket(command, unrelated) {
		t.Fatal("unrelated listener on the same numeric port was accepted")
	}
	wildcard := hostfacts.ListenerSocketIdentityV1{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 2222, Wildcard: true}
	if !dropbearCommandCoversSocket([]string{"/usr/sbin/dropbear", "-F", "-p", "2222"}, wildcard) {
		t.Fatal("bare Dropbear port did not cover its wildcard socket")
	}
}

func TestDropbearCommandRequiresCompleteRuntimeListenerSet(t *testing.T) {
	command := []string{"/usr/sbin/dropbear", "-F", "-p", "2222"}
	v4 := hostfacts.ListenerSocketIdentityV1{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "0.0.0.0", Port: 2222,
		Wildcard: true, CoverageFamilies: []hostfacts.Family{hostfacts.FamilyIPv4}}
	v6 := hostfacts.ListenerSocketIdentityV1{Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv6, Bind: "::", Port: 2222,
		Wildcard: true, CoverageFamilies: []hostfacts.Family{hostfacts.FamilyIPv6}}
	if dropbearCommandExactlyCoversSockets(command, []hostfacts.ListenerSocketIdentityV1{v4}) {
		t.Fatal("a partially established wildcard listener set was accepted")
	}
	if !dropbearCommandExactlyCoversSockets(command, []hostfacts.ListenerSocketIdentityV1{v4, v6}) {
		t.Fatal("the complete wildcard listener set was rejected")
	}
	if dropbearCommandExactlyCoversSockets(command, []hostfacts.ListenerSocketIdentityV1{v4, v6, v6}) {
		t.Fatal("duplicate runtime listener authority was accepted")
	}
	explicit := []string{"/usr/sbin/dropbear", "-F", "-p", "0.0.0.0:2222"}
	if !dropbearCommandExactlyCoversSockets(explicit, []hostfacts.ListenerSocketIdentityV1{v4}) ||
		dropbearCommandExactlyCoversSockets(explicit, []hostfacts.ListenerSocketIdentityV1{v4, v6}) {
		t.Fatal("an explicit IPv4 wildcard did not retain its address family")
	}
}

func TestDropbearPostureComesFromCorrelatedRuntimeCommand(t *testing.T) {
	section := uciSection{Internal: "section-A", Type: dropbearConfigName, Options: map[string]uciOption{
		"PasswordAuth":      {Values: []string{"0"}},
		"RootPasswordAuth":  {Values: []string{"0"}},
		"RootLogin":         {Values: []string{"1"}},
		"LocalPortForward":  {Values: []string{"0"}},
		"RemotePortForward": {Values: []string{"1"}},
		"GatewayPorts":      {Values: []string{"1"}},
		"MaxAuthTries":      {Values: []string{"5"}},
	}}
	command := []string{"/usr/sbin/dropbear", "-F", "-P", "/var/run/dropbear.section-A.pid", "-p", "2001:db8::10:2222",
		"-s", "-g", "-j", "-a", "-T", "5"}
	authentication, forwarding, err := projectDropbearRuntimePosture([]uciSection{section}, []dropbearInstanceEvidence{{Command: command}})
	if err != nil {
		t.Fatal(err)
	}
	if authentication.PasswordAuthentication != "no" || authentication.PermitRootLogin != "prohibit-password" ||
		authentication.MaxAuthTries != 5 || len(authentication.AuthenticationMethods) != 1 ||
		forwarding.AllowTCPForwarding != "remote" || forwarding.GatewayPorts != "yes" {
		t.Fatalf("authentication=%#v forwarding=%#v", authentication, forwarding)
	}
	section.Options["PasswordAuth"] = uciOption{Values: []string{"1"}}
	if _, _, err := projectDropbearRuntimePosture([]uciSection{section}, []dropbearInstanceEvidence{{Command: command}}); err == nil {
		t.Fatal("stale procd security command was accepted for changed UCI")
	}
	section.Options["PasswordAuth"] = uciOption{Values: []string{"0"}}
	section.Options["MaxAuthTries"] = uciOption{Values: []string{"0"}}
	if _, _, err := projectDropbearRuntimePosture([]uciSection{section}, []dropbearInstanceEvidence{{Command: command}}); err == nil {
		t.Fatal("compile-default MaxAuthTries was represented without exact runtime evidence")
	}
}

func TestDropbearPolicyProjectionAndExactSectionCheckpoint(t *testing.T) {
	tries, disabled, enabled := uint16(5), false, true
	desired := domain.DesiredPolicyV1{Schema: domain.PolicySchemaV1, MaxAuthTries: &tries, PasswordAuthentication: &disabled,
		PermitRootLogin: domain.RootLoginProhibitPassword, PubkeyAuthentication: &enabled}
	prepared, policy, err := prepareDropbearUCIPolicy(desired)
	if err != nil {
		t.Fatal(err)
	}
	before := uciSection{Name: "main", Internal: "main", Type: "dropbear", Options: map[string]uciOption{
		"Port": {Values: []string{"22"}}, "ForceCommand": {Values: []string{"internal command"}}, "keyfile": {List: true, Values: []string{"/a", "/b"}},
	}}
	after := cloneUCISection(before)
	digest := prepared.ArtifactDigest
	applyDropbearPolicy(&after, policy, digest)
	if dropbearArtifactDigest(after) != digest || after.Options["Port"].Values[0] != "22" || after.Options["ForceCommand"].Values[0] != "internal command" || len(after.Options["keyfile"].Values) != 2 {
		t.Fatalf("projected section=%#v", after)
	}
	restored := cloneUCISection(after)
	restored.Options = cloneUCISection(before).Options
	if !dropbearOptionsEqual(restored.Options, before.Options) {
		t.Fatal("exact selected-section checkpoint did not restore")
	}
	unrelatedBefore := dropbearConfig{Sections: []uciSection{before, {Name: "other", Internal: "other", Type: "dropbear", Options: map[string]uciOption{"Port": {Values: []string{"2222"}}}}}}
	unrelatedAfter := dropbearConfig{Sections: []uciSection{after, cloneUCISection(unrelatedBefore.Sections[1])}}
	if !dropbearUnrelatedSectionsEqual(unrelatedBefore, unrelatedAfter, "main") {
		t.Fatal("selected-section change was mistaken for unrelated mutation")
	}
	unrelatedAfter.Sections[1].Options["Port"] = uciOption{Values: []string{"2200"}}
	if dropbearUnrelatedSectionsEqual(unrelatedBefore, unrelatedAfter, "main") {
		t.Fatal("unrelated Dropbear section mutation was not detected")
	}
	grace, keyboard := uint32(30), true
	for _, unsupported := range []domain.DesiredPolicyV1{
		{Schema: domain.PolicySchemaV1, PermitRootLogin: domain.RootLoginUnchanged, LoginGraceTimeSeconds: &grace},
		{Schema: domain.PolicySchemaV1, PermitRootLogin: domain.RootLoginUnchanged, PubkeyAuthentication: &disabled},
		{Schema: domain.PolicySchemaV1, PermitRootLogin: domain.RootLoginUnchanged, KbdInteractiveAuthentication: &keyboard},
	} {
		if _, _, err := prepareDropbearUCIPolicy(unsupported); err == nil {
			t.Fatalf("unsupported policy accepted: %#v", unsupported)
		}
	}
}

func TestDropbearRollbackUsesBrokerCheckpointAuthorityAndRejectsForgedOptionMap(t *testing.T) {
	tries, disabled, enabled := uint16(5), false, true
	prepared, policy, err := prepareDropbearUCIPolicy(domain.DesiredPolicyV1{Schema: domain.PolicySchemaV1, MaxAuthTries: &tries,
		PasswordAuthentication: &disabled, PermitRootLogin: domain.RootLoginProhibitPassword, PubkeyAuthentication: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	before := uciSection{Name: "main", Internal: "main", Type: dropbearConfigName, Options: map[string]uciOption{
		"Port": {Values: []string{"22"}}, "UnrelatedCustom": {Values: []string{"preserve me"}}, "keyfile": {List: true, Values: []string{"/a", "/b"}},
	}}
	candidateDigest := prepared.ArtifactDigest
	priorDigest := dropbearArtifactDigest(before)
	checkpoint := dropbearCheckpoint{Schema: dropbearCheckpointV1, OperationID: "ssh-operation:authority", EndpointID: endpointID("ipv4", 22),
		CandidateArtifactDigest: candidateDigest, PriorArtifactDigest: priorDigest, Configuration: domain.Revision("configuration"),
		SectionFingerprint: dropbearSectionFingerprint(before), Section: cloneUCISection(before)}
	checkpointBytes, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	prior := dropbearPrior(checkpointBytes, priorDigest)
	staged := StageResultV1{ArtifactDigest: candidateDigest, EndpointID: checkpoint.EndpointID, Prior: prior, ProviderRevision: ProviderRevision, ConfigurationRevision: domain.Revision("staged")}
	payload, payloadDigest, err := broker.MarshalPayload(staged)
	if err != nil {
		t.Fatal(err)
	}
	authority := completedMutationAuthorityFake{response: broker.Response{OK: true, Payload: payload, PayloadDigest: payloadDigest}}
	host := &dropbearUCIHost{rollbackAuthority: authority}
	envelope := broker.Request{OperationID: checkpoint.OperationID, Fence: broker.Fence{Resource: "ssh-managed-dropin"}}
	request := RestoreRequestV1{ExpectedCurrentArtifactDigest: candidateDigest, EndpointID: checkpoint.EndpointID, Prior: prior}

	resolved, resolvedPrior, err := host.authoritativeCheckpoint(envelope, request)
	if err != nil || resolvedPrior.Digest != priorDigest || !dropbearOptionsEqual(resolved.Section.Options, before.Options) {
		t.Fatalf("valid broker checkpoint was not resolved: prior=%#v err=%v", resolvedPrior, err)
	}
	if _, _, err := host.authoritativeCheckpoint(envelope, request); err != nil {
		t.Fatalf("persisted broker checkpoint was not repeat-readable for crash recovery: %v", err)
	}
	after := cloneUCISection(before)
	applyDropbearPolicy(&after, policy, candidateDigest)
	after.Options = cloneUCISection(resolved.Section).Options
	if !dropbearOptionsEqual(after.Options, before.Options) || after.Options["UnrelatedCustom"].Values[0] != "preserve me" {
		t.Fatal("valid broker checkpoint did not recover the exact prior option map")
	}

	forgedCheckpoint := checkpoint
	forgedCheckpoint.Section = cloneUCISection(checkpoint.Section)
	forgedCheckpoint.Section.Options["ArbitraryInjected"] = uciOption{Values: []string{"accepted only by forged caller state"}}
	forgedCheckpoint.SectionFingerprint = dropbearSectionFingerprint(forgedCheckpoint.Section)
	forgedCheckpoint.PriorArtifactDigest = dropbearArtifactDigest(forgedCheckpoint.Section)
	forgedBytes, err := json.Marshal(forgedCheckpoint)
	if err != nil {
		t.Fatal(err)
	}
	forged := dropbearPrior(forgedBytes, forgedCheckpoint.PriorArtifactDigest)
	request.Prior = forged
	if _, _, err := host.authoritativeCheckpoint(envelope, request); brokerFailureCode(err) != broker.CodeInvalidRequest {
		t.Fatalf("forged caller checkpoint was not rejected: %v", err)
	}

	request.Prior = prior
	otherEndpoint := request
	otherEndpoint.EndpointID = endpointID("ipv4", 2222)
	if _, _, err := host.authoritativeCheckpoint(envelope, otherEndpoint); brokerFailureCode(err) != broker.CodeRecoveryRequired {
		t.Fatalf("checkpoint for another endpoint was accepted: %v", err)
	}
	otherOperation := envelope
	otherOperation.OperationID = "ssh-operation:other"
	if _, _, err := host.authoritativeCheckpoint(otherOperation, request); brokerFailureCode(err) != broker.CodeRecoveryRequired {
		t.Fatalf("checkpoint for another operation was accepted: %v", err)
	}
	otherCandidate := request
	otherCandidate.ExpectedCurrentArtifactDigest = domain.Revision("other-candidate")
	if _, _, err := host.authoritativeCheckpoint(envelope, otherCandidate); brokerFailureCode(err) != broker.CodeRecoveryRequired {
		t.Fatalf("checkpoint for another staged candidate was accepted: %v", err)
	}

	host.rollbackAuthority = completedMutationAuthorityFake{err: errors.New("missing")}
	if _, _, err := host.authoritativeCheckpoint(envelope, request); brokerFailureCode(err) != broker.CodeRecoveryRequired {
		t.Fatalf("missing server checkpoint did not fail closed: %v", err)
	}
}

func TestDropbearSecurityLogRequiresFreshPIDSocketCorrelationAndBounds(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	remote := netip.MustParseAddrPort("192.0.2.4:50123")
	valid := "Wed Jan  1 00:00:00 2027 [1800000000.123] daemon.notice dropbear[321]: Pubkey auth succeeded for 'alice' with ssh-ed25519 key SHA256:redacted from 192.0.2.4:50123\n"
	observed, err := parseDropbearAuthLog([]byte(valid), "alice", remote, 321, now)
	if err != nil || observed != now.UnixMilli()+123 {
		t.Fatalf("observed=%d err=%v", observed, err)
	}
	for _, test := range []struct {
		name string
		data string
		pid  int
	}{
		{"wrong-pid", valid, 322},
		{"wrong-user", strings.Replace(valid, "'alice'", "'mallory'", 1), 321},
		{"stale", strings.Replace(valid, "1800000000.123", "1799999000.123", 1), 321},
		{"unrelated-process", strings.Replace(valid, "dropbear[321]", "sshd[321]", 1), 321},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseDropbearAuthLog([]byte(test.data), "alice", remote, test.pid, now); err == nil {
				t.Fatal("uncorrelated security-log evidence was accepted")
			}
		})
	}
	if _, err := parseDropbearAuthLog([]byte(strings.Repeat("x", maxCommandOutput+1)), "alice", remote, 321, now); err == nil {
		t.Fatal("oversized Dropbear log evidence was accepted")
	}
}
