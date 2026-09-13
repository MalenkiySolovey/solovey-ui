package privilegedbroker

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTransportModeSelectionIsExplicitAndClosed(t *testing.T) {
	for argument, expected := range map[string]TransportMode{
		"--transport=systemd-activated": SystemdActivated,
		"--transport=standalone-owned":  StandaloneOwned,
	} {
		actual, err := ParseTransportArgs([]string{argument})
		if err != nil || actual != expected {
			t.Fatalf("ParseTransportArgs(%q) = %q, %v", argument, actual, err)
		}
	}
	for _, arguments := range [][]string{nil, {}, {"--transport=standalone-owned", "extra"}, {"--transport=auto"}, {"STANDALONE_OWNED"}} {
		if _, err := ParseTransportArgs(arguments); err == nil {
			t.Fatalf("unsafe transport arguments accepted: %#v", arguments)
		}
	}
	if _, err := OpenTransport(TransportMode("AUTO")); err == nil {
		t.Fatal("unknown transport mode was accepted")
	}
}

func TestManifestLocationFollowsTheExplicitBrokerTransport(t *testing.T) {
	for mode, expected := range map[TransportMode]string{
		SystemdActivated: DefaultManifest,
		StandaloneOwned:  RuntimeManifestPath,
	} {
		actual, err := ManifestPathForTransport(mode)
		if err != nil || actual != expected {
			t.Fatalf("ManifestPathForTransport(%q) = %q, %v", mode, actual, err)
		}
	}
	if _, err := ManifestPathForTransport(TransportMode("AUTO")); err == nil {
		t.Fatal("unknown transport selected a broker manifest path")
	}
	if RuntimeManifestPath != StandaloneSocketRoot+"/broker-clients.json" || DefaultManifest == RuntimeManifestPath {
		t.Fatal("persistent and runtime broker authority locations are not separated")
	}
}

func TestReadinessLoopIsBoundedAndRequiresFreshSuccess(t *testing.T) {
	var attempts atomic.Int32
	err := waitForReadiness(context.Background(), time.Second, func(context.Context) error {
		if attempts.Add(1) < 3 {
			return errors.New("not ready")
		}
		return nil
	})
	if err != nil || attempts.Load() != 3 {
		t.Fatalf("readiness success = attempts %d, err %v", attempts.Load(), err)
	}
	started := time.Now()
	err = waitForReadiness(context.Background(), 150*time.Millisecond, func(context.Context) error { return errors.New("down") })
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("readiness timeout = %v after %v", err, time.Since(started))
	}
	if err := waitForReadiness(context.Background(), 0, func(context.Context) error { return nil }); err == nil {
		t.Fatal("zero readiness timeout was accepted")
	}
}

func TestProcdServiceProjectionIsStrictAndBounded(t *testing.T) {
	expected := procdManifestClientFixture()
	valid := map[string]any{
		"solovey-ui": map[string]any{"instances": map[string]any{
			"root-broker": map[string]any{"running": true, "pid": 40, "command": []string{"/usr/lib/solovey-ui/solovey-privileged-broker", "--transport=standalone-owned"}, "user": "root", "group": "root"},
			"panel":       map[string]any{"running": true, "pid": 55, "command": expected.ProcdCommand, "user": expected.ProcdUser, "group": expected.ProcdGroup, "term_timeout": 30},
		}},
	}
	raw, _ := json.Marshal(valid)
	evidence, err := parseProcdInstanceEvidence(raw, expected)
	if err != nil || evidence.PID != 55 {
		t.Fatalf("valid procd evidence = %#v, %v", evidence, err)
	}

	mutations := map[string]func(map[string]any){
		"wrong-service": func(value map[string]any) { value["other"] = value["solovey-ui"]; delete(value, "solovey-ui") },
		"extra-service": func(value map[string]any) { value["other"] = map[string]any{} },
		"wrong-instance": func(value map[string]any) {
			instances(value)["other"] = instances(value)["panel"]
			delete(instances(value), "panel")
		},
		"stopped":       func(value map[string]any) { panel(value)["running"] = false },
		"wrong-pid":     func(value map[string]any) { panel(value)["pid"] = 1 },
		"wrong-command": func(value map[string]any) { panel(value)["command"] = []string{"/bin/false"} },
		"wrong-user":    func(value map[string]any) { panel(value)["user"] = "root" },
		"wrong-group":   func(value map[string]any) { panel(value)["group"] = "root" },
	}
	for name, mutate := range mutations {
		copy := cloneJSONMap(t, valid)
		mutate(copy)
		raw, _ := json.Marshal(copy)
		if _, err := parseProcdInstanceEvidence(raw, expected); err == nil {
			t.Fatalf("%s procd drift was accepted", name)
		}
	}
	duplicate := `{"solovey-ui":{"instances":{"panel":{"running":true,"running":true,"pid":55,"command":["/usr/lib/solovey-ui/solovey-broker-readiness"],"user":"solovey-ui","group":"solovey-ui"}}}}`
	if _, err := parseProcdInstanceEvidence([]byte(duplicate), expected); err == nil {
		t.Fatal("duplicate procd JSON member was accepted")
	}
	if _, err := parseProcdInstanceEvidence([]byte(strings.Repeat("x", maxProcdEvidenceBytes+1)), expected); err == nil {
		t.Fatal("oversized procd evidence was accepted")
	}
}

func TestProcdProcessBindingRejectsPIDStartAndAncestryDrift(t *testing.T) {
	identity := PeerIdentity{PID: 55, StartTime: "900"}
	if err := validateProcdProcessBinding(identity, 55, "900", ProcdRelationMain, false); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		pid      int
		start    string
		relation string
		descends bool
	}{
		{56, "900", ProcdRelationMain, false},
		{55, "901", ProcdRelationMain, false},
		{55, "900", ProcdRelationAncestor, true},
		{40, "800", ProcdRelationAncestor, false},
		{40, "800", "name-only", true},
	} {
		if err := validateProcdProcessBinding(identity, test.pid, test.start, test.relation, test.descends); err == nil {
			t.Fatalf("unsafe procd process binding accepted: %#v", test)
		}
	}
	if err := validateProcdProcessBinding(identity, 40, "800", ProcdRelationAncestor, true); err != nil {
		t.Fatal(err)
	}
}

func TestManifestSupervisorProofIsIndependentFromTransport(t *testing.T) {
	systemd := Manifest{Schema: ManifestSchemaSystemd}
	procd := Manifest{Schema: ManifestSchemaProcd}
	systemdProof, systemdErr := systemd.SupervisorProof()
	procdProof, procdErr := procd.SupervisorProof()
	if systemdErr != nil || procdErr != nil || systemdProof != SupervisorProofSystemd || procdProof != SupervisorProofProcd {
		t.Fatalf("manifest proof selection = %q/%v, %q/%v", systemdProof, systemdErr, procdProof, procdErr)
	}
	for _, mode := range []TransportMode{SystemdActivated, StandaloneOwned} {
		if !mode.Valid() {
			t.Fatalf("transport %q unexpectedly invalid", mode)
		}
	}

	client := procdManifestClientFixture()
	identity := PeerIdentity{PID: 55, UID: client.UID, GID: client.GID, Executable: client.Executable,
		ExecutableDigest: client.ExecutableDigest, Device: client.Device, Inode: client.Inode,
		Supervisor: "procd", ProcdService: client.ProcdService, ProcdInstance: client.ProcdInstance,
		SupervisorRelation: client.ProcdRelation}
	manifest := Manifest{Clients: []ClientManifest{client}}
	if _, ok := manifest.matching(RolePanel, identity); !ok {
		t.Fatal("exact procd manifest identity did not match")
	}
	for _, mutate := range []func(*PeerIdentity){
		func(value *PeerIdentity) { value.ExecutableDigest = strings.Repeat("f", 64) },
		func(value *PeerIdentity) { value.Device++ },
		func(value *PeerIdentity) { value.Inode++ },
		func(value *PeerIdentity) { value.ProcdService = "other" },
		func(value *PeerIdentity) { value.ProcdInstance = "other" },
		func(value *PeerIdentity) { value.SupervisorRelation = ProcdRelationAncestor },
	} {
		drift := identity
		mutate(&drift)
		if _, ok := manifest.matching(RolePanel, drift); ok {
			t.Fatalf("drifted procd manifest identity matched: %#v", drift)
		}
	}
}

func TestSystemdProofPreservesReleasedUnpinnedSSHUnitCompatibility(t *testing.T) {
	for _, test := range []struct {
		expected string
		observed string
		allowed  bool
	}{
		{"solovey-ui.service", "solovey-ui.service", true},
		{"solovey-ui.service", "other.service", false},
		{"", "ssh.service", true},
		{"", "sshd.service", true},
		{"", "", false},
	} {
		if actual := systemdCgroupProofMatches(test.expected, test.observed); actual != test.allowed {
			t.Fatalf("systemdCgroupProofMatches(%q, %q) = %t", test.expected, test.observed, actual)
		}
	}
}

func TestAuthenticatedAncestorProjectionSelectsExactlyOneCurrentProcdInstance(t *testing.T) {
	expected := ClientManifest{Roles: []Role{RoleSSHProof}, ProcdService: "dropbear", ProcdInstanceSelector: ProcdSelectorUniqueAncestor,
		ProcdRelation: ProcdRelationAncestor, ProcdCommand: []string{"/usr/sbin/dropbear", "-F"}, ProcdUser: "root", ProcdGroup: "root",
		ProcdExecutable: "/usr/sbin/dropbear", ProcdExecutableDigest: strings.Repeat("d", 64), ProcdDevice: 7, ProcdInode: 11}
	raw := []byte(`{"dropbear":{"instances":{"cfg001":{"running":true,"pid":41,"command":["/usr/sbin/dropbear","-F","-P","/var/run/dropbear.cfg001.pid","-p","22"]},"cfg002":{"running":true,"pid":42,"command":["/usr/sbin/dropbear","-F","-P","/var/run/dropbear.cfg002.pid","-p","2222"]}}}}`)
	evidence, err := parseProcdAncestorEvidence(raw, expected, func(pid int) bool { return pid == 42 })
	if err != nil || evidence.Name != "cfg002" || evidence.PID != 42 {
		t.Fatalf("evidence=%#v err=%v", evidence, err)
	}
	if _, err := parseProcdAncestorEvidence(raw, expected, func(int) bool { return true }); err == nil {
		t.Fatal("ambiguous Dropbear ancestry was accepted")
	}
	if _, err := parseProcdAncestorEvidence(raw, expected, func(int) bool { return false }); err == nil {
		t.Fatal("missing Dropbear ancestry was accepted")
	}
}

func TestProcdAncestorProofUsesAuthenticatedCompositionFactsWithoutDaemonHardcoding(t *testing.T) {
	expected := ClientManifest{Roles: []Role{RoleSSHProof}, ProcdService: "tinysshd", ProcdInstanceSelector: ProcdSelectorUniqueAncestor,
		ProcdRelation: ProcdRelationAncestor, ProcdCommand: []string{"/usr/sbin/tinysshd", "-D"}, ProcdUser: "root", ProcdGroup: "root",
		ProcdExecutable: "/usr/sbin/tinysshd", ProcdExecutableDigest: strings.Repeat("e", 64), ProcdDevice: 9, ProcdInode: 13}
	if err := validateProcdManifestIdentity(expected); runtime.GOOS == "linux" && err != nil {
		t.Fatalf("non-Dropbear authenticated procd proposition was rejected: %v", err)
	}
	raw := []byte(`{"tinysshd":{"instances":{"main":{"running":true,"pid":51,"command":["/usr/sbin/tinysshd","-D","--port","22"]}}}}`)
	if evidence, err := parseProcdAncestorEvidence(raw, expected, func(pid int) bool { return pid == 51 }); err != nil || evidence.Name != "main" {
		t.Fatalf("authenticated proposition was not observed: evidence=%#v err=%v", evidence, err)
	}
	drifted := expected
	drifted.ProcdCommand = []string{"/usr/sbin/tinysshd", "--foreground"}
	if _, err := parseProcdAncestorEvidence(raw, drifted, func(pid int) bool { return pid == 51 }); err == nil {
		t.Fatal("runtime command drift from the authenticated proposition was accepted")
	}
}

func procdManifestClientFixture() ClientManifest {
	return ClientManifest{Name: "panel", UID: 1001, GID: 1001, Executable: "/usr/lib/solovey-ui/solovey-ui",
		ExecutableDigest: strings.Repeat("a", 64), Device: 7, Inode: 11, Roles: []Role{RolePanel},
		ProcdService: "solovey-ui", ProcdInstance: "panel", ProcdCommand: []string{"/usr/lib/solovey-ui/solovey-broker-readiness"},
		ProcdUser: "solovey-ui", ProcdGroup: "solovey-ui", ProcdRelation: ProcdRelationMain}
}

func instances(value map[string]any) map[string]any {
	return value["solovey-ui"].(map[string]any)["instances"].(map[string]any)
}

func panel(value map[string]any) map[string]any { return instances(value)["panel"].(map[string]any) }

func cloneJSONMap(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(value)
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
