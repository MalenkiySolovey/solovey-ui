//go:build linux

package privilegedbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/procdexec"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
)

func TestProcdRuntimeAttestationUsesRealSO_PEERCREDAndProcIdentity(t *testing.T) {
	manifest, client := currentProcessProcdManifest(t, uint32(os.Geteuid()), uint32(os.Getegid()))
	attestor, err := NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	attestor.supervision = testProcdSupervision(client, os.Getpid(), "/services/solovey-ui/panel")

	serverConnection, clientConnection := unixConnectionPair(t)
	defer serverConnection.Close()
	defer clientConnection.Close()
	peer, err := attestor.Attest(context.Background(), serverConnection, RolePanel)
	if err != nil {
		t.Fatal(err)
	}
	if peer.PID != os.Getpid() || peer.Executable != client.Executable || peer.Supervisor != "procd" ||
		peer.ProcdService != "solovey-ui" || peer.ProcdInstance != "panel" || peer.SupervisorCgroup != "/services/solovey-ui/panel" {
		t.Fatalf("runtime peer=%#v", peer)
	}
	if err := attestor.Recheck(context.Background(), peer, RolePanel); err != nil {
		t.Fatal(err)
	}
	wrongBoot := peer
	wrongBoot.BootID = "different-boot"
	if err := attestor.Recheck(context.Background(), wrongBoot, RolePanel); err == nil || peerAttestationClass(err) != PeerAttestationBootMismatch {
		t.Fatalf("boot drift err=%v class=%q", err, peerAttestationClass(err))
	}
	for name, ids := range map[string][2]uint32{
		"uid": {peer.UID + 1, peer.GID},
		"gid": {peer.UID, peer.GID + 1},
	} {
		if _, err := attestor.inspect(context.Background(), peer.PID, ids[0], ids[1], RolePanel); err == nil || peerAttestationClass(err) != PeerAttestationUIDGIDMismatch {
			t.Fatalf("%s drift err=%v class=%q", name, err, peerAttestationClass(err))
		}
	}
}

func TestProcdRuntimeAttestationRejectsEveryBoundIdentityDrift(t *testing.T) {
	manifest, client := currentProcessProcdManifest(t, uint32(os.Geteuid()), uint32(os.Getegid()))
	identity, err := inspectCommonPeer(os.Getpid(), uint32(os.Geteuid()), uint32(os.Getegid()), manifest.Revision)
	if err != nil {
		t.Fatal(err)
	}
	validRaw := procdRuntimeJSON(t, client, os.Getpid())
	type driftCase struct {
		manifest Manifest
		raw      []byte
		pid      int
		cgroup   string
		role     Role
		class    PeerAttestationClass
	}
	tests := map[string]driftCase{
		"wrong service":  {manifest, []byte(`{"other":{"instances":{}}}`), os.Getpid(), "/services/solovey-ui/panel", RolePanel, PeerAttestationSupervisionMismatch},
		"wrong instance": {manifest, []byte(`{"solovey-ui":{"instances":{"other":{"running":true,"pid":2,"command":["/usr/lib/solovey-ui/solovey-broker-readiness"],"user":"solovey-ui","group":"solovey-ui"}}}}`), os.Getpid(), "/services/solovey-ui/panel", RolePanel, PeerAttestationSupervisionMismatch},
		"wrong pid":      {manifest, validRaw, os.Getpid() + 1, "/services/solovey-ui/panel", RolePanel, PeerAttestationSupervisionMismatch},
		"wrong cgroup":   {manifest, validRaw, os.Getpid(), "/services/other/panel", RolePanel, PeerAttestationCgroupPolicyMismatch},
		"wrong role":     {manifest, validRaw, os.Getpid(), "/services/solovey-ui/panel", RoleSSHProof, PeerAttestationRoleNotAuthorized},
	}
	addExecutableDrift := func(name string, mutate func(*ClientManifest)) {
		t.Helper()
		drifted := manifest
		drifted.Clients = append([]ClientManifest(nil), manifest.Clients...)
		mutate(&drifted.Clients[0])
		drifted, err = FinalizeManifest(drifted)
		if err != nil {
			t.Fatal(err)
		}
		tests[name] = driftCase{drifted, validRaw, os.Getpid(), "/services/solovey-ui/panel", RolePanel, PeerAttestationExecutableMismatch}
	}
	addExecutableDrift("wrong digest", func(client *ClientManifest) { client.ExecutableDigest = strings.Repeat("f", 64) })
	addExecutableDrift("wrong device", func(client *ClientManifest) { client.Device++ })
	addExecutableDrift("wrong inode", func(client *ClientManifest) { client.Inode++ })

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			attestor, err := NewManifestAttestor(test.manifest)
			if err != nil {
				t.Fatal(err)
			}
			attestor.supervision = procdSupervisionAttestor{
				inspector: &ProcdInspector{run: func(context.Context, ClientManifest) ([]byte, error) {
					if test.pid != os.Getpid() {
						var value map[string]any
						if json.Unmarshal(test.raw, &value) == nil {
							if service, ok := value["solovey-ui"].(map[string]any); ok {
								if instances, ok := service["instances"].(map[string]any); ok {
									if panel, ok := instances["panel"].(map[string]any); ok {
										panel["pid"] = test.pid
										test.raw, _ = json.Marshal(value)
									}
								}
							}
						}
					}
					return test.raw, nil
				}},
				observeCgroup: func(int, string, string) (procdCgroupEvidence, error) {
					return testProcdCgroupEvidence(test.cgroup, "solovey-ui", "panel")
				},
			}
			_, err = attestor.inspect(context.Background(), identity.PID, identity.UID, identity.GID, test.role)
			if err == nil || peerAttestationClass(err) != test.class {
				t.Fatalf("err=%v class=%q want=%q", err, peerAttestationClass(err), test.class)
			}
		})
	}
}

func TestSystemdRuntimeAttestationPreservesExactUnitSecurity(t *testing.T) {
	identity, err := inspectCommonPeer(os.Getpid(), uint32(os.Geteuid()), uint32(os.Getegid()), strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	client := ClientManifest{Name: "panel", UID: identity.UID, GID: identity.GID, Executable: identity.Executable,
		ExecutableDigest: identity.ExecutableDigest, Device: identity.Device, Inode: identity.Inode,
		CgroupUnit: "solovey-ui.service", CgroupPolicy: CgroupRequired, CgroupAuthorityRevision: CgroupAuthorityRevisionV1, Roles: []Role{RolePanel}}
	manifest, err := FinalizeManifest(Manifest{Schema: ManifestSchemaSystemd, Clients: []ClientManifest{client}})
	if err != nil {
		t.Fatal(err)
	}
	attestor, err := NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	attestor.supervision = systemdSupervisionAttestor{readCgroup: func(int) (systemdCgroupEvidence, error) {
		return systemdCgroupEvidence{unit: "solovey-ui.service", availability: processevidence.CgroupAvailable, revision: strings.Repeat("c", 64)}, nil
	}}
	peer, err := attestor.inspect(context.Background(), os.Getpid(), identity.UID, identity.GID, RolePanel)
	if err != nil || peer.CgroupUnit != "solovey-ui.service" || peer.Supervisor != "systemd" {
		t.Fatalf("valid systemd peer=%#v err=%v", peer, err)
	}
	attestor.supervision = systemdSupervisionAttestor{readCgroup: func(int) (systemdCgroupEvidence, error) {
		return systemdCgroupEvidence{unit: "other.service", availability: processevidence.CgroupAvailable, revision: strings.Repeat("c", 64)}, nil
	}}
	if _, err := attestor.inspect(context.Background(), os.Getpid(), identity.UID, identity.GID, RolePanel); err == nil || peerAttestationClass(err) != PeerAttestationSupervisionMismatch {
		t.Fatalf("wrong systemd unit err=%v class=%q", err, peerAttestationClass(err))
	}
}

func TestSupervisorCgroupAdaptersRemainSemanticallyDistinct(t *testing.T) {
	if path, available, err := parseProcdCgroup([]byte("0::/services/solovey-ui/panel\n"), "solovey-ui", "panel"); err != nil || !available || path != "/services/solovey-ui/panel" {
		t.Fatalf("procd cgroup=%q available=%t err=%v", path, available, err)
	}
	for _, evidence := range []string{
		"0::/services/other/panel\n",
		"0::/services/solovey-ui/other\n",
	} {
		if _, available, err := parseProcdCgroup([]byte(evidence), "solovey-ui", "panel"); err == nil || !available {
			t.Fatalf("drifted procd cgroup accepted: %q", evidence)
		}
	}
	if path, available, err := parseProcdCgroup([]byte("0::/\n"), "solovey-ui", "panel"); err != nil || available || path != "" {
		t.Fatalf("optional procd cgroup=%q available=%t err=%v", path, available, err)
	}
	if unit, err := parseSystemdCgroupUnit([]byte("0::/system.slice/solovey-ui.service\n")); err != nil || unit != "solovey-ui.service" {
		t.Fatalf("systemd unit=%q err=%v", unit, err)
	}
	if _, err := parseSystemdCgroupUnit([]byte("0::/services/solovey-ui/panel\n")); err == nil {
		t.Fatal("procd service cgroup was accepted as a systemd unit")
	}
}

func TestInitialUserNamespaceValidationRejectsRemap(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"uid_map", "gid_map"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("0 100000 65536\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateInitialUserNamespace(root); err == nil {
		t.Fatal("remapped user namespace was accepted")
	}
}

func TestOpenWrtManifestWriterReaderRuntimeAuthorization(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root broker parent is required to launch the real non-root SO_PEERCRED peer")
	}
	const peerUID = 65534
	const peerGID = 65534
	manifest, client := currentProcessProcdManifest(t, peerUID, peerGID)
	makeTestExecutableTraversable(t, client.Executable)
	root, err := os.MkdirTemp("/root", "solovey-broker-manifest-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	manifestPath := filepath.Join(root, "broker-clients.json")
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(data, '\n'), 0o640); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	attestor, err := NewManifestAttestor(loaded)
	if err != nil {
		t.Fatal(err)
	}
	var peerPID atomic.Int64
	attestor.supervision = procdSupervisionAttestor{
		inspector: &ProcdInspector{run: func(context.Context, ClientManifest) ([]byte, error) {
			pid := int(peerPID.Load())
			if pid <= 1 {
				return nil, errors.New("test peer PID is unavailable")
			}
			return procdRuntimeJSON(t, client, pid), nil
		}},
		observeCgroup: func(int, string, string) (procdCgroupEvidence, error) {
			return procdCgroupEvidence{path: "/services/solovey-ui/panel", availability: processevidence.CgroupAvailable, revision: strings.Repeat("c", 64)}, nil
		},
	}
	bootBytes, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Fatal(err)
	}
	bootID := strings.TrimSpace(string(bootBytes))
	server, err := NewServer(NewRegistry(), memoryJournal{}, attestor, bootID)
	if err != nil {
		t.Fatal(err)
	}
	socketRoot, err := os.MkdirTemp("", "solovey-broker-runtime-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketRoot) })
	if err := os.Chmod(socketRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(socketRoot, "broker.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(socketPath, 0o666); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener, RolePanel) }()

	command := exec.Command(os.Args[0], "-test.run=^TestOpenWrtRuntimePeerHelper$")
	command.Env = append(os.Environ(), "SUI_OPENWRT_RUNTIME_PEER_HELPER=1", "SUI_OPENWRT_RUNTIME_SOCKET="+socketPath, "SUI_OPENWRT_RUNTIME_BOOT="+bootID)
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: peerUID, Gid: peerGID}}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	peerPID.Store(int64(command.Process.Pid))
	if err := command.Wait(); err != nil {
		t.Fatalf("runtime peer failed: %v\n%s", err, output.String())
	}
	cancel()
	_ = listener.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func makeTestExecutableTraversable(t *testing.T, executable string) {
	t.Helper()
	temporaryRoot := filepath.Clean(os.TempDir())
	for directory := filepath.Dir(executable); strings.HasPrefix(directory, temporaryRoot+string(filepath.Separator)); directory = filepath.Dir(directory) {
		info, err := os.Stat(directory)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory, info.Mode().Perm()|0o111); err != nil {
			t.Fatal(err)
		}
		if directory == temporaryRoot || filepath.Dir(directory) == directory {
			break
		}
	}
}

func TestOpenWrtRuntimePeerHelper(t *testing.T) {
	if os.Getenv("SUI_OPENWRT_RUNTIME_PEER_HELPER") != "1" {
		return
	}
	time.Sleep(50 * time.Millisecond)
	socketPath, bootID := os.Getenv("SUI_OPENWRT_RUNTIME_SOCKET"), os.Getenv("SUI_OPENWRT_RUNTIME_BOOT")
	client := &Client{SocketPath: socketPath, Role: RolePanel, BootID: bootID, Now: time.Now,
		Dial: func(ctx context.Context, path string) (net.Conn, error) {
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", path)
		}}
	var capabilities CapabilitiesV1
	if _, err := client.Invoke(context.Background(), Call{Verb: VerbCapabilities, OperationID: "openwrt-runtime-peer", Purpose: "test", Timeout: time.Second, Payload: struct{}{}}, &capabilities); err != nil {
		t.Fatal(err)
	}
	if capabilities.Role != RolePanel || capabilities.Revision == "" {
		t.Fatalf("capabilities=%#v", capabilities)
	}
}

func currentProcessProcdManifest(t *testing.T, uid, gid uint32) (Manifest, ClientManifest) {
	t.Helper()
	identity, err := inspectCommonPeer(os.Getpid(), uint32(os.Geteuid()), uint32(os.Getegid()), strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	client := ClientManifest{Name: "broker-readiness", UID: uid, GID: gid, Executable: identity.Executable,
		ExecutableDigest: identity.ExecutableDigest, Device: identity.Device, Inode: identity.Inode, Roles: []Role{RolePanel},
		CgroupPolicy: CgroupOptional, CgroupAuthorityRevision: CgroupAuthorityRevisionV1,
		ProcdService: "solovey-ui", ProcdInstance: "panel", ProcdCommand: []string{"/usr/lib/solovey-ui/solovey-broker-readiness"},
		ProcdUser: "solovey-ui", ProcdGroup: "solovey-ui", ProcdRelation: ProcdRelationMain, CapabilitiesOnly: true}
	manifest, err := FinalizeManifest(Manifest{Schema: ManifestSchemaProcd, ApplicationOwnerRevision: strings.Repeat("2", 64), Clients: []ClientManifest{client}})
	if err != nil {
		t.Fatal(err)
	}
	return manifest, client
}

func testProcdSupervision(client ClientManifest, pid int, cgroup string) procdSupervisionAttestor {
	return procdSupervisionAttestor{
		inspector: &ProcdInspector{run: func(context.Context, ClientManifest) ([]byte, error) {
			return procdRuntimeJSON(nil, client, pid), nil
		}},
		observeCgroup: func(int, string, string) (procdCgroupEvidence, error) {
			return testProcdCgroupEvidence(cgroup, client.ProcdService, client.ProcdInstance)
		},
	}
}

func testProcdCgroupEvidence(path, service, instance string) (procdCgroupEvidence, error) {
	observed, available, err := parseProcdCgroup([]byte("0::"+path+"\n"), service, instance)
	if err != nil {
		return procdCgroupEvidence{}, err
	}
	availability := processevidence.CgroupUnavailable
	if available {
		availability = processevidence.CgroupAvailable
	}
	return procdCgroupEvidence{path: observed, availability: availability, revision: strings.Repeat("c", 64)}, nil
}

func procdRuntimeJSON(t *testing.T, client ClientManifest, pid int) []byte {
	value := map[string]any{client.ProcdService: map[string]any{"instances": map[string]any{client.ProcdInstance: map[string]any{
		"running": true, "pid": pid, "command": client.ProcdCommand, "user": client.ProcdUser, "group": client.ProcdGroup,
	}}}}
	raw, err := json.Marshal(value)
	if err != nil {
		if t != nil {
			t.Fatal(err)
		}
		panic(err)
	}
	return raw
}

func unixConnectionPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	server, err := listener.AcceptUnix()
	if err != nil {
		client.Close()
		t.Fatal(err)
	}
	return server, client
}

func TestProcdExecutableCandidatePolicyIsBounded(t *testing.T) {
	if procdexec.PrimaryPath != "/bin/ubus" || procdexec.LegacyPath != "/sbin/ubus" {
		t.Fatalf("unexpected procd executable candidates: %q %q", procdexec.PrimaryPath, procdexec.LegacyPath)
	}
}

func TestPeerAttestationClassIsClosedAndPathFree(t *testing.T) {
	err := attestationFailure(PeerAttestationSupervisionMismatch, fmt.Errorf("secret path %s", filepath.Join("/root", strconv.Itoa(os.Getpid()))))
	if class := peerAttestationClass(err); class != PeerAttestationSupervisionMismatch || strings.Contains(string(class), "/") {
		t.Fatalf("class=%q", class)
	}
}

type classifiedRejectingAttestor struct{}

func (classifiedRejectingAttestor) Attest(context.Context, *net.UnixConn, Role) (PeerIdentity, error) {
	return PeerIdentity{}, attestationFailure(PeerAttestationExecutableMismatch, errors.New("private executable path"))
}

func (classifiedRejectingAttestor) Recheck(context.Context, PeerIdentity, Role) error { return nil }

func TestDeniedPeerAuditIncludesOnlyBoundedAttestationClass(t *testing.T) {
	server, err := NewServer(NewRegistry(), memoryJournal{}, classifiedRejectingAttestor{}, "boot")
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan AuditEvent, 1)
	server.Audit = func(event AuditEvent) { events <- event }
	path := filepath.Join(t.TempDir(), "denial.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener, RolePanel) }()
	connection, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var response Response
	if err := ReadFrame(connection, &response, MaxResponseBytes); err != nil || response.Code != CodeUnauthorized {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	select {
	case event := <-events:
		encoded, _ := json.Marshal(event)
		if event.ResultClass != "denied_peer" || event.PeerAttestation != string(PeerAttestationExecutableMismatch) || strings.Contains(string(encoded), "private executable") {
			t.Fatalf("event=%s", encoded)
		}
	case <-time.After(time.Second):
		t.Fatal("denied peer audit was not emitted")
	}
	cancel()
	_ = listener.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
