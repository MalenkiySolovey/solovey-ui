//go:build linux

package privilegedbroker

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"golang.org/x/sys/unix"
)

func TestSystemdSupervisorEvidenceTransitionFailsFinalRecheck(t *testing.T) {
	manifestRevision := Digest([]byte("systemd-generation"))
	identity, err := inspectCommonPeer(os.Getpid(), uint32(os.Geteuid()), uint32(os.Getegid()), manifestRevision)
	if err != nil {
		t.Fatal(err)
	}
	client := ClientManifest{Name: "panel", UID: identity.UID, GID: identity.GID, Executable: identity.Executable,
		ExecutableDigest: identity.ExecutableDigest, Device: identity.Device, Inode: identity.Inode,
		CgroupUnit: "init.scope", CgroupPolicy: CgroupRequired, CgroupAuthorityRevision: CgroupAuthorityRevisionV1,
		Roles: []Role{RolePanel}}
	manifest, err := FinalizeManifest(Manifest{Schema: ManifestSchemaSystemd, Clients: []ClientManifest{client}})
	if err != nil {
		t.Fatal(err)
	}
	attestor, err := NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	revision := Digest([]byte("systemd-supervisor-first"))
	attestor.supervision = systemdSupervisionAttestor{readCgroup: func(int) (systemdCgroupEvidence, error) {
		return systemdCgroupEvidence{unit: "init.scope", availability: processevidence.CgroupAvailable, revision: revision}, nil
	}}
	peer, err := attestor.inspect(context.Background(), os.Getpid(), identity.UID, identity.GID, RolePanel)
	if err != nil {
		t.Fatal(err)
	}
	bindTestPidfd(t, &peer)
	revision = Digest([]byte("systemd-supervisor-second"))
	if err := attestor.Recheck(context.Background(), peer, RolePanel); err == nil || peerAttestationClass(err) != PeerAttestationCgroupPolicyMismatch {
		t.Fatalf("error=%v class=%q", err, peerAttestationClass(err))
	}
}

func TestProcdInstanceReplacementAndStartIdentityDriftFailFinalRecheck(t *testing.T) {
	manifest, client := currentProcessProcdManifest(t, uint32(os.Geteuid()), uint32(os.Getegid()))
	manifest.Clients[0].CapabilitiesOnly = false
	manifest, err := FinalizeManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	attestor, err := NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	supervisorPID := os.Getpid()
	attestor.supervision = procdSupervisionAttestor{
		inspector: &ProcdInspector{run: func(context.Context, ClientManifest) ([]byte, error) {
			return procdRuntimeJSON(nil, client, supervisorPID), nil
		}},
		observeCgroup: func(int, string, string) (procdCgroupEvidence, error) {
			return procdCgroupEvidence{path: "/services/solovey-ui/panel", availability: processevidence.CgroupAvailable,
				revision: strings.Repeat("c", 64)}, nil
		},
	}
	peer, err := attestor.inspect(context.Background(), os.Getpid(), uint32(os.Geteuid()), uint32(os.Getegid()), RolePanel)
	if err != nil {
		t.Fatal(err)
	}
	bindTestPidfd(t, &peer)
	driftedStart := peer
	driftedStart.StartTime = "1"
	if err := attestor.Recheck(context.Background(), driftedStart, RolePanel); err == nil || peerAttestationClass(err) != PeerAttestationStartIdentityMismatch {
		t.Fatalf("start error=%v class=%q", err, peerAttestationClass(err))
	}
	child := exec.Command(os.Args[0], "-test.run=^TestPidfdLivenessHelper$")
	child.Env = append(os.Environ(), "SUI_PIDFD_LIVENESS_HELPER=1")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	}()
	supervisorPID = child.Process.Pid
	if err := attestor.Recheck(context.Background(), peer, RolePanel); err == nil || peerAttestationClass(err) != PeerAttestationSupervisionMismatch {
		t.Fatalf("replacement error=%v class=%q", err, peerAttestationClass(err))
	}
}

func bindTestPidfd(t *testing.T, peer *PeerIdentity) {
	t.Helper()
	fd, err := unix.PidfdOpen(peer.PID, 0)
	if err != nil {
		t.Fatal(err)
	}
	peer.livenessFD, peer.hasLiveness = fd, true
	t.Cleanup(func() { _ = unix.Close(fd) })
}
