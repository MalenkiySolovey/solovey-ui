//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing/common/control"
	"golang.org/x/sys/unix"
)

// The namespace has no usable egress. This proof exercises the real
// transport and attestor, not the installed Panel or a live firewall fence.
func TestSystemdZapretSocketBroker(t *testing.T) {
	path := os.Getenv("SOLOVEY_TEST_ZAPRET_SOCKET")
	unit := os.Getenv("SOLOVEY_TEST_ZAPRET_PEER_UNIT")
	if path == "" || unit == "" {
		t.Skip("requires the disposable systemd socket runner")
	}
	insistZapretServiceIsolation(t, 0, "00000000000830db")
	iface, err := net.InterfaceByName("zproof0")
	if err != nil || iface.Flags&net.FlagUp != 0 {
		t.Fatalf("disposable egress must exist and be down: %v", err)
	}
	pid, err := strconv.Atoi(os.Getenv("SOLOVEY_TEST_ZAPRET_PEER_PID"))
	if err != nil || pid <= 1 {
		t.Fatal("missing systemd MainPID")
	}
	self, err := inspectCommonPeer(os.Getpid(), 0, 0, strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := FinalizeManifest(Manifest{Schema: ManifestSchemaSystemd, Clients: []ClientManifest{{
		Name: "panel", UID: 65534, GID: 65534, Roles: []Role{RolePanel}, Executable: self.Executable,
		ExecutableDigest: self.ExecutableDigest, Device: self.Device, Inode: self.Inode, CgroupUnit: unit,
		CgroupPolicy: CgroupRequired, CgroupAuthorityRevision: CgroupAuthorityRevisionV1,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	attestor, err := NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	peer, err := attestor.inspect(ctx, pid, 65534, 65534, RolePanel)
	if err != nil {
		t.Fatalf("attest expected systemd MainPID: %v", err)
	}
	srv, err := NewServer(NewRegistry(), memoryJournal{}, attestor, self.BootID)
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := socketNetNSCookie(openZapretServiceSocket(t, unix.SOCK_STREAM))
	if err != nil {
		t.Fatal(err)
	}
	scope := testZapretSocketScope(t, cookie, int32(iface.Index))
	grant := ZapretSocketGrant{Scope: scope, Generation: strings.Repeat("2", 64), PeerRevision: peer.Revision, Expires: time.Now().Add(25 * time.Second)}
	events := make(chan AuditEvent, 8)
	srv.Audit = func(event AuditEvent) { events <- event }
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := preparePeerListener(listener); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 0, 65534); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o660); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- srv.ServeZapretSockets(ctx, listener, grant, func(_ context.Context, got ZapretSocketGrant, _ int, admit func() error) error {
			if got.Generation != grant.Generation || got.Scope.PlanRevision() != scope.PlanRevision() {
				return errors.New("test grant changed")
			}
			return admit()
		})
	}()
	want := map[string]int{"success": 1, "denied_peer": 1, "denied_validation_failed": 2}
	for range 4 {
		select {
		case event := <-events:
			t.Logf("SOCKET_ADMISSION phase=%s result=%s attestation=%s", event.Phase, event.ResultClass, event.PeerAttestation)
			if want[event.ResultClass] == 0 || event.Verb != auditZapretSocket {
				t.Fatalf("unexpected admission result: %#v", event)
			}
			want[event.ResultClass]--
		case err := <-done:
			t.Fatalf("socket server stopped before all requests: %v", err)
		case <-ctx.Done():
			t.Fatalf("missing admission results: %v", want)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	t.Log("SYSTEMD_CAPLESS_PROVIDER_SOCKET_HANDOFF=PASS; WRONG_UNIT_WRONG_PROTOCOL_AND_MARKED_REPLAY=DENIED")
}

func TestSystemdZapretSocketProvider(t *testing.T) {
	path := os.Getenv("SOLOVEY_TEST_ZAPRET_SOCKET")
	if path == "" || os.Getenv("SOLOVEY_TEST_ZAPRET_PEER_UNIT") != "" {
		t.Skip("integration client only")
	}
	insistZapretServiceIsolation(t, 65534, "0000000000000000")
	deadline := time.Now().Add(15 * time.Second)
	for {
		info, err := os.Lstat(path)
		if err == nil && info.Mode()&os.ModeSocket != 0 && info.Mode().Perm() == 0o660 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("broker socket not ready")
		}
		time.Sleep(25 * time.Millisecond)
	}
	protect := func(fd int) error {
		// The duplicate is the same kernel socket. os.File owns only the copy.
		copyFD, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		file := os.NewFile(uintptr(copyFD), "provider-socket")
		defer file.Close()
		raw, err := file.SyscallConn()
		if err != nil {
			t.Fatal(err)
		}
		return control.ProtectPath(path)("tcp4", "192.0.2.1:443", raw)
	}
	tcp := openZapretServiceSocket(t, unix.SOCK_STREAM)
	if err := unix.SetsockoptInt(tcp, unix.SOL_SOCKET, unix.SO_MARK, 1); !errors.Is(err, unix.EPERM) {
		t.Fatalf("unprivileged client attempted SO_MARK: %v", err)
	}
	if os.Getenv("SOLOVEY_TEST_ZAPRET_WRONG_UNIT") == "1" {
		if err := protect(tcp); err == nil {
			t.Fatal("identical UID/GID and executable in another unit received a grant")
		}
		return
	}
	if err := protect(openZapretServiceSocket(t, unix.SOCK_DGRAM)); err == nil {
		t.Fatal("UDP received a TCP-only grant")
	}
	if err := protect(tcp); err != nil {
		t.Fatalf("pinned provider protect_path: %v", err)
	}
	iface, err := net.InterfaceByName("zproof0")
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := socketNetNSCookie(tcp)
	if err != nil {
		t.Fatal(err)
	}
	scope := testZapretSocketScope(t, cookie, int32(iface.Index))
	for opt, want := range map[int]int{unix.SO_MARK: int(scope.Mark()), unix.SO_BINDTOIFINDEX: iface.Index} {
		if got, err := unix.GetsockoptInt(tcp, unix.SOL_SOCKET, opt); err != nil || got != want {
			t.Fatalf("socket option %d: got %d, want %d (%v)", opt, got, want, err)
		}
	}
	if err := protect(tcp); err == nil {
		t.Fatal("a marked socket received a second grant")
	}
}

func openZapretServiceSocket(t *testing.T, kind int) int {
	t.Helper()
	fd, err := unix.Socket(unix.AF_INET, kind|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return fd
}

func insistZapretServiceIsolation(t *testing.T, uid int, mask string) {
	t.Helper()
	if os.Getuid() != uid || os.Getgid() != uid {
		t.Fatal("unexpected systemd service UID/GID")
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"NoNewPrivs:\t1\n", "CapEff:\t" + mask + "\n", "CapBnd:\t" + mask + "\n", "CapAmb:\t" + mask + "\n"} {
		if !strings.Contains(string(status), "\n"+line) {
			t.Fatalf("missing service policy: %q", line)
		}
	}
	parent := os.Getenv("SOLOVEY_TEST_ZAPRET_PARENT_NETNS")
	self, err := os.Readlink("/proc/self/ns/net")
	if err != nil || !strings.HasPrefix(parent, "net:[") || self == parent {
		t.Fatal("refusing to test admission in the host network namespace")
	}
}
