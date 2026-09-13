//go:build linux

package privilegedbroker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"golang.org/x/sys/unix"
)

func TestProcdCgroupRequiredOptionalPolicyMatrix(t *testing.T) {
	manifest, client := currentProcessProcdManifest(t, uint32(os.Geteuid()), uint32(os.Getegid()))
	identity, err := inspectCommonPeer(os.Getpid(), uint32(os.Geteuid()), uint32(os.Getegid()), manifest.Revision)
	if err != nil {
		t.Fatal(err)
	}
	baseInspector := &ProcdInspector{run: func(context.Context, ClientManifest) ([]byte, error) {
		return procdRuntimeJSON(nil, client, os.Getpid()), nil
	}}
	for _, test := range []struct {
		name         string
		policy       CgroupPolicy
		availability processevidence.CgroupAvailability
		path         string
		observeErr   error
		wantOK       bool
	}{
		{"required_present_matching", CgroupRequired, processevidence.CgroupAvailable, "/services/solovey-ui/panel", nil, true},
		{"required_absent", CgroupRequired, processevidence.CgroupUnavailable, "", nil, false},
		{"optional_present_matching", CgroupOptional, processevidence.CgroupAvailable, "/services/solovey-ui/panel", nil, true},
		{"optional_absent", CgroupOptional, processevidence.CgroupUnavailable, "", nil, true},
		{"present_wrong_service", CgroupOptional, processevidence.CgroupAvailable, "", errors.New("service mismatch"), false},
		{"present_wrong_instance", CgroupOptional, processevidence.CgroupAvailable, "", errors.New("instance mismatch"), false},
		{"present_unsafe_authority", CgroupOptional, processevidence.CgroupUnsafe, "", errors.New("unsafe authority"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := client
			candidate.CgroupPolicy = test.policy
			attestor := procdSupervisionAttestor{inspector: baseInspector, observeCgroup: func(int, string, string) (procdCgroupEvidence, error) {
				return procdCgroupEvidence{path: test.path, availability: test.availability, revision: Digest([]byte(test.name))}, test.observeErr
			}}
			bound, bindErr := attestor.Bind(context.Background(), identity, candidate, manifest.Revision)
			if test.wantOK {
				if bindErr != nil || bound.CgroupAvailability != string(test.availability) || bound.CgroupPolicy != test.policy {
					t.Fatalf("bound=%+v err=%v", bound, bindErr)
				}
				return
			}
			if bindErr == nil || peerAttestationClass(bindErr) != PeerAttestationCgroupPolicyMismatch {
				t.Fatalf("error=%v class=%q", bindErr, peerAttestationClass(bindErr))
			}
		})
	}
}

func TestSystemdRequiredCgroupAbsenceFailsClosed(t *testing.T) {
	client := ClientManifest{CgroupPolicy: CgroupRequired, CgroupAuthorityRevision: CgroupAuthorityRevisionV1, CgroupUnit: "solovey-ui.service"}
	attestor := systemdSupervisionAttestor{readCgroup: func(int) (systemdCgroupEvidence, error) {
		return systemdCgroupEvidence{availability: processevidence.CgroupUnavailable, revision: Digest([]byte("absent"))}, nil
	}}
	if _, err := attestor.Bind(context.Background(), PeerIdentity{PID: os.Getpid()}, client, ""); err == nil || peerAttestationClass(err) != PeerAttestationCgroupPolicyMismatch {
		t.Fatalf("error=%v class=%q", err, peerAttestationClass(err))
	}
}

func TestCgroupAuthorityFilesystemOwnershipAndAncestryValidation(t *testing.T) {
	if !safeRootCgroupDirectory("/sys/fs/cgroup") || !cgroup2Filesystem("/sys/fs/cgroup") {
		t.Fatal("Linux cgroup-v2 root did not satisfy the production authority checks")
	}
	root, err := os.MkdirTemp("/root", "solovey-cgroup-authority-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if !safeRootCgroupDirectory(root) {
		t.Fatal("root-owned non-writable ancestry was rejected")
	}
	if cgroup2Filesystem(root) {
		t.Fatal("ordinary filesystem was accepted as cgroup-v2 authority")
	}
	writable := filepath.Join(root, "writable")
	if err := os.Mkdir(writable, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(writable, 0o777); err != nil {
		t.Fatal(err)
	}
	if safeRootCgroupDirectory(writable) {
		t.Fatal("writable cgroup ancestry was accepted")
	}
	foreign := filepath.Join(root, "foreign")
	if err := os.Mkdir(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(foreign, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if safeRootCgroupDirectory(foreign) {
		t.Fatal("non-root-owned cgroup authority was accepted")
	}
	for _, path := range []string{"/services/other/panel", "/services/solovey-ui/other"} {
		if _, _, err := parseProcdCgroup([]byte("0::"+path+"\n"), "solovey-ui", "panel"); err == nil {
			t.Fatalf("mismatched membership %q was accepted", path)
		}
	}
}

func TestCgroupAvailabilityTransitionIsClassifiedBeforeDispatch(t *testing.T) {
	manifest, client := currentProcessProcdManifest(t, uint32(os.Geteuid()), uint32(os.Getegid()))
	attestor, err := NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	state := processevidence.CgroupAvailable
	attestor.supervision = procdSupervisionAttestor{
		inspector: &ProcdInspector{run: func(context.Context, ClientManifest) ([]byte, error) {
			return procdRuntimeJSON(nil, client, os.Getpid()), nil
		}},
		observeCgroup: func(int, string, string) (procdCgroupEvidence, error) {
			path := "/services/solovey-ui/panel"
			if state == processevidence.CgroupUnavailable {
				path = ""
			}
			return procdCgroupEvidence{path: path, availability: state, revision: Digest([]byte("cgroup-" + string(state)))}, nil
		},
	}
	peer, err := attestor.inspect(context.Background(), os.Getpid(), uint32(os.Geteuid()), uint32(os.Getegid()), RolePanel)
	if err != nil {
		t.Fatal(err)
	}
	pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		t.Fatal(err)
	}
	peer.livenessFD, peer.hasLiveness = pidfd, true
	defer unix.Close(pidfd)
	state = processevidence.CgroupUnavailable
	if err := attestor.Recheck(context.Background(), peer, RolePanel); err == nil || peerAttestationClass(err) != PeerAttestationCgroupPolicyMismatch {
		t.Fatalf("error=%v class=%q", err, peerAttestationClass(err))
	}
	if !strings.Contains(string(peerAttestationClass(attestationFailure(PeerAttestationCgroupPolicyMismatch, errors.New("private")))), "cgroup") {
		t.Fatal("cgroup denial class is not closed and explicit")
	}
}
