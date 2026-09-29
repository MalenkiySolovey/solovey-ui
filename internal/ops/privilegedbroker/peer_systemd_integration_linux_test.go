//go:build linux

package privilegedbroker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Run by tests/installer/broker-pidfd-runtime.sh under the production broker
// capability and hardening policy. No attestor or supervisor is substituted.
func TestSystemdCrossUIDPidfdAttestation(t *testing.T) {
	unit := os.Getenv("SOLOVEY_TEST_PIDFD_UNIT")
	if unit == "" {
		t.Skip("requires the explicit real-systemd integration runner")
	}
	if os.Getuid() != 0 {
		t.Fatal("broker must have UID 0")
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"CapPrm:", "CapEff:", "CapBnd:", "CapAmb:"} {
		found := false
		for _, line := range strings.Split(string(status), "\n") {
			if !strings.HasPrefix(line, field) {
				continue
			}
			value, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, field)), 16, 64)
			if err != nil || value != 0x830cb {
				t.Fatalf("production capability contract changed: %s", line)
			}
			found = true
			t.Log(line)
		}
		if !found {
			t.Fatalf("missing %s", field)
		}
	}
	if !strings.Contains(string(status), "NoNewPrivs:\t1") {
		t.Fatal("NoNewPrivileges absent")
	}
	address := fmt.Sprintf("@solovey-pidfd-test-%d", os.Getpid())
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: address, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestCrossUIDPidfdClient$")
	child.Env = append(os.Environ(), "SOLOVEY_TEST_PIDFD_CLIENT="+address)
	child.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534, Groups: []uint32{}}}
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	// The helper has a bounded lifetime even if accept/attestation fails. The
	// constrained broker never needs permission to signal it for cleanup.
	defer func() { _ = child.Wait() }()
	connection, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatal(err)
	}
	self, err := inspectCommonPeer(os.Getpid(), 0, 0, strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := FinalizeManifest(Manifest{Schema: ManifestSchemaSystemd, Clients: []ClientManifest{{
		Name: "panel", UID: 65534, GID: 65534, Executable: self.Executable,
		ExecutableDigest: self.ExecutableDigest, Device: self.Device, Inode: self.Inode,
		CgroupUnit: unit, CgroupPolicy: CgroupRequired, CgroupAuthorityRevision: CgroupAuthorityRevisionV1, Roles: []Role{RolePanel},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	attestor, err := NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := attestor.Attest(context.Background(), connection, RolePanel)
	if err != nil {
		t.Fatalf("live cross-UID attestation: %v", err)
	}
	defer attestor.ClosePeer(peer)
	if peer.PID != child.Process.Pid || peer.UID != 65534 || peer.GID != 65534 || peer.CgroupUnit != unit {
		t.Fatal("SO_PEERCRED/production cgroup identity mismatch")
	}
	if err := unix.PidfdSendSignal(peer.livenessFD, 0, nil, 0); !errors.Is(err, unix.EPERM) {
		t.Fatalf("old signal-0 primitive: got %v, want EPERM", err)
	}
	if _, err := connection.Write([]byte("?")); err != nil {
		t.Fatal(err)
	}
	var reply [1]byte
	if _, err := io.ReadFull(connection, reply[:]); err != nil || reply[0] != 'A' {
		t.Fatalf("live peer roundtrip: %v", err)
	}
	if err := attestor.Recheck(context.Background(), peer, RolePanel); err != nil {
		t.Fatal(err)
	}
	if err := attestor.VerifyWriter(context.Background(), peer, WriterCredentials{PID: peer.PID, UID: peer.UID, GID: peer.GID}); err != nil {
		t.Fatal(err)
	}
	t.Log("PIDFD_SIGNAL0_ALIVE_WITHOUT_CAP_KILL=EPERM; LIVE_CROSS_UID_PEER=PASS; BROKER_CAP_KILL=ABSENT")
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := attestor.Recheck(context.Background(), peer, RolePanel); peerAttestationClass(err) != PeerAttestationConnectorDeath {
		t.Fatalf("dead peer: %v", err)
	}
	old := peer
	old.PID = os.Getpid()
	if err := attestor.Recheck(context.Background(), old, RolePanel); peerAttestationClass(err) != PeerAttestationConnectorDeath {
		t.Fatalf("numeric PID substitution: %v", err)
	}
	duplicate, err := unix.Dup(peer.livenessFD)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Close(duplicate); err != nil {
		t.Fatal(err)
	}
	invalid := peer
	invalid.livenessFD = duplicate
	if err := peerAlive(invalid); peerAttestationClass(err) != PeerAttestationLivenessUnavailable {
		t.Fatalf("closed pidfd: %v", err)
	}
	t.Log("DEAD_PEER_DETECTION=PASS; PID_REUSE_AUTHORITY=PASS; INVALID_PIDFD_CLASSIFICATION=PASS")
}

func TestCrossUIDPidfdClient(t *testing.T) {
	address := os.Getenv("SOLOVEY_TEST_PIDFD_CLIENT")
	if address == "" {
		t.Skip("integration child only")
	}
	if os.Getuid() != 65534 || os.Getgid() != 65534 {
		t.Fatal("client is not nonroot")
	}
	connection, err := net.DialTimeout("unix", address, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var request [1]byte
	for {
		_, err := connection.Read(request[:])
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := connection.Write([]byte("A")); err != nil {
			t.Fatal(err)
		}
	}
}
