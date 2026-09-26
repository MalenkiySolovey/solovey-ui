//go:build linux

package privilegedbroker

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/executableobject"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"golang.org/x/sys/unix"
)

func TestSystemdSSHProofSetgidPeer(t *testing.T) {
	if socket := os.Getenv("SOLOVEY_TEST_PROOF_SOCKET"); socket != "" {
		if os.Getuid() != 12346 || os.Geteuid() != 12346 || os.Getgid() != 12346 || os.Getegid() != 12345 {
			t.Fatalf("setgid identities: uid=%d euid=%d gid=%d egid=%d", os.Getuid(), os.Geteuid(), os.Getgid(), os.Getegid())
		}
		gid := os.Getegid()
		if err := unix.Setresgid(gid, gid, gid); err != nil {
			t.Fatal(err)
		}
		connection, err := net.Dial("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		_, _ = io.Copy(io.Discard, connection)
		return
	}
	if os.Geteuid() != 0 {
		t.Fatal("canonical setgid proof gate requires root")
	}
	// /run is commonly nosuid. Exercise setgid on the native persistent mount.
	root, err := os.MkdirTemp("/var/lib", "solovey-proof-peer-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "proof")
	data, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(executable, 0, 12345); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(executable, SystemdSSHProofMode); err != nil {
		t.Fatal(err)
	}
	entry := ClientManifest{Name: "ssh-proof", AnyNonRootUID: true, AnyGID: true, RequiredGroup: 12345,
		Roles: []Role{RoleSSHProof}, CgroupPolicy: CgroupRequired, CgroupAuthorityRevision: CgroupAuthorityRevisionV1}
	policy, err := SystemdClientExecutablePolicy(entry)
	if err != nil {
		t.Fatal(err)
	}
	object, err := executableobject.Open(executable, policy)
	if err != nil {
		t.Fatal(err)
	}
	identity := object.Identity()
	policy.RequiredOwner.GID++
	if err := object.Revalidate(); err != nil {
		t.Fatalf("caller mutated frozen object policy: %v", err)
	}
	_ = object.Close()
	entry.Executable, entry.ExecutableDigest, entry.Device, entry.Inode = executable, identity.Digest, identity.Device, identity.Inode
	manifest, err := FinalizeManifest(Manifest{Schema: ManifestSchemaSystemd, Clients: []ClientManifest{entry}})
	if err != nil {
		t.Fatal(err)
	}
	attestor, err := NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	// WSL supplies real process/socket evidence; the existing systemd projection
	// unit tests own service/scope parsing. No physical systemd claim is made.
	attestor.supervision = systemdSupervisionAttestor{readCgroup: func(int) (systemdCgroupEvidence, error) {
		return systemdCgroupEvidence{unit: "session-proof.scope", availability: processevidence.CgroupAvailable, revision: strings.Repeat("a", 64)}, nil
	}}
	socket := filepath.Join(root, "proof.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chown(socket, 0, 12345); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(socket, 0o660); err != nil {
		t.Fatal(err)
	}
	_ = listener.SetDeadline(time.Now().Add(15 * time.Second))
	command := exec.Command(executable, "-test.run=^TestSystemdSSHProofSetgidPeer$")
	command.Env = append(os.Environ(), "SOLOVEY_TEST_PROOF_SOCKET="+socket)
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 12346, Gid: 12346, Groups: []uint32{12346}}}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	connection, err := listener.AcceptUnix()
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("proof connection: %v: %s", err, output.String())
	}
	defer connection.Close()
	peer, err := attestor.Attest(context.Background(), connection, RoleSSHProof)
	if err != nil {
		t.Fatal(err)
	}
	defer attestor.ClosePeer(peer)
	if peer.UID != 12346 || peer.GID != 12345 || peer.ExecutableUID != 0 || peer.ExecutableGID != 12345 {
		t.Fatalf("peer=%+v", peer)
	}
	if err := attestor.Recheck(context.Background(), peer, RoleSSHProof); err != nil {
		t.Fatal(err)
	}
	rootPeer := peer
	rootPeer.UID = 0
	if len(manifest.commonMatching(RoleSSHProof, rootPeer)) != 0 {
		t.Fatal("root caller admitted by AnyNonRootUID")
	}
	if _, err := attestor.Attest(context.Background(), connection, RolePanel); err == nil {
		t.Fatal("proof executable admitted as panel")
	}
	if err := os.Chown(executable, 0, 12347); err != nil {
		t.Fatal(err)
	}
	if err := attestor.Recheck(context.Background(), peer, RoleSSHProof); err == nil {
		t.Fatal("executable owner drift accepted")
	}
	_ = connection.Close()
	if err := command.Wait(); err != nil {
		t.Fatalf("proof child: %v %s", err, output.String())
	}
}
