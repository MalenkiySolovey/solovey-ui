//go:build linux

package privilegedbroker

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestStandaloneTransportOwnsBothFixedRolesAndRejectsCollision(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("TARGET_EVIDENCE_REQUIRED: root ownership is required for standalone namespace tests")
	}
	root := t.TempDir()
	const socketGID = 1
	if err := os.Chown(root, 0, socketGID); err != nil || os.Chmod(root, 0o750) != nil {
		t.Fatal("prepare root-owned socket fixture")
	}
	set, err := openStandaloneTransportAt(root, socketGID)
	if err != nil {
		t.Fatal(err)
	}
	for role, name := range map[Role]string{RolePanel: filepath.Base(DefaultSocketPath), RoleSSHProof: filepath.Base(ProofSocketPath)} {
		listener := set.Listeners[role]
		if listener == nil || listener.Addr().String() != filepath.Join(root, name) {
			t.Fatalf("role %q listener = %#v", role, listener)
		}
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("role %q socket stat: %v", role, err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || info.Mode().Perm() != 0o660 || stat.Uid != 0 || stat.Gid != socketGID {
			t.Fatalf("role %q socket identity is unsafe", role)
		}
	}
	if _, err := openStandaloneTransportAt(root, socketGID); err == nil {
		t.Fatal("second standalone broker acquired active sockets")
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{filepath.Base(DefaultSocketPath), filepath.Base(ProofSocketPath)} {
		if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("owned socket %q remained after close", name)
		}
	}
}

func TestStandaloneTransportReplacesOnlyExpectedStaleSocket(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("TARGET_EVIDENCE_REQUIRED: root ownership is required for standalone namespace tests")
	}
	root := t.TempDir()
	const socketGID = 1
	if err := os.Chown(root, 0, socketGID); err != nil || os.Chmod(root, 0o750) != nil {
		t.Fatal("prepare root-owned socket fixture")
	}
	path := filepath.Join(root, filepath.Base(DefaultSocketPath))
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil || os.Chown(path, 0, socketGID) != nil || os.Chmod(path, 0o660) != nil {
		t.Fatal("prepare expected stale socket")
	}
	set, err := openStandaloneTransportAt(root, socketGID)
	if err != nil {
		t.Fatalf("expected stale socket was not replaced: %v", err)
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneTransportRefusesWrongStaleSocketMode(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("TARGET_EVIDENCE_REQUIRED: root ownership is required for standalone namespace tests")
	}
	root := t.TempDir()
	const socketGID = 1
	if err := os.Chown(root, 0, socketGID); err != nil || os.Chmod(root, 0o750) != nil {
		t.Fatal("prepare root-owned socket fixture")
	}
	path := filepath.Join(root, filepath.Base(DefaultSocketPath))
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil || os.Chown(path, 0, socketGID) != nil || os.Chmod(path, 0o600) != nil {
		t.Fatal("prepare wrong-mode stale socket")
	}
	if _, err := openStandaloneTransportAt(root, socketGID); err == nil {
		t.Fatal("wrong-mode stale socket was removed")
	}
}

func TestStandaloneTransportRefusesUnknownStaleObjects(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("TARGET_EVIDENCE_REQUIRED: root ownership is required for standalone namespace tests")
	}
	const socketGID = 1
	for name, prepare := range map[string]func(string) error{
		"regular": func(path string) error { return os.WriteFile(path, []byte("foreign"), 0o660) },
		"symlink": func(path string) error { return os.Symlink("missing", path) },
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chown(root, 0, socketGID); err != nil || os.Chmod(root, 0o750) != nil {
				t.Fatal("prepare root-owned socket fixture")
			}
			if err := prepare(filepath.Join(root, filepath.Base(DefaultSocketPath))); err != nil {
				t.Fatal(err)
			}
			if _, err := openStandaloneTransportAt(root, socketGID); err == nil {
				t.Fatal("unknown stale object was removed")
			}
		})
	}
}
