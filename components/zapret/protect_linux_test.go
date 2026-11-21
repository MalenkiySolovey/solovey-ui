//go:build linux && zapretproof && !minimal

package zapret

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	M "github.com/sagernet/sing/common/metadata"
	"golang.org/x/sys/unix"
)

const zapretProbeUID = 65534

type zapretProtectInput struct {
	Configuration json.RawMessage
	TCP, UDP      string
	Unavailable   bool
}

// This uses a short-lived FD receiver in the disposable fixture, not a new
// production broker verb or a general service for privileged socket options.
func qualifyZapretProtectPath(t *testing.T, ctx context.Context, directA, directB json.RawMessage, mark uint32, tcp, udp string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sui-zapret-protect-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chown(dir, 0, zapretProbeUID); err != nil || os.Chmod(dir, 0o750) != nil {
		t.Fatal("could not isolate the protect_path directory", err)
	}
	path := filepath.Join(dir, "dial.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chown(path, 0, zapretProbeUID); err != nil || os.Chmod(path, 0o660) != nil {
		t.Fatal("could not restrict the protect_path socket", err)
	}
	listenerRaw, err := listener.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var passCredErr error
	if err := listenerRaw.Control(func(fd uintptr) {
		passCredErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_PASSCRED, 1)
	}); err != nil || passCredErr != nil {
		t.Fatal("could not require writer credentials", err, passCredErr)
	}
	var options map[string]json.RawMessage
	if err := json.Unmarshal(directA, &options); err != nil {
		t.Fatal(err)
	}
	delete(options, "routing_mark")
	options["protect_path"], _ = json.Marshal(path)
	projected, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	input := zapretProtectInput{Configuration: json.RawMessage(fmt.Sprintf(`{"log":{"disabled":true},"outbounds":[%s,%s],"route":{"final":"direct-b"}}`, projected, directB)), TCP: tcp, UDP: udp}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	self, err := processevidence.Observe(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := broker.FinalizeManifest(broker.Manifest{Schema: broker.ManifestSchemaSystemd, Clients: []broker.ClientManifest{{
		Name: "zapret-proof-core", UID: zapretProbeUID, GID: zapretProbeUID, Executable: self.Executable,
		ExecutableDigest: self.ExeDigest, Device: self.ExeDevice, Inode: self.ExeInode,
		CgroupUnit: os.Getenv("SOLOVEY_TEST_BROKER_UNIT"), CgroupPolicy: broker.CgroupRequired,
		CgroupAuthorityRevision: broker.CgroupAuthorityRevisionV1, Roles: []broker.Role{broker.RolePanel},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	attestor, err := broker.NewManifestAttestor(manifest)
	if err != nil {
		t.Fatal(err)
	}
	run := func() (*exec.Cmd, *bytes.Buffer) {
		t.Helper()
		data, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, "/usr/bin/setpriv", "--reuid=65534", "--regid=65534", "--clear-groups",
			"--bounding-set=-all", "--inh-caps=-all", "--ambient-caps=-all", "--no-new-privs",
			exe, "-test.run=^TestZapretCaplessPeer$", "-test.v", "-test.timeout=10s")
		cmd.Env = []string{"SOLOVEY_TEST_PROTECT_PEER=" + os.Getenv("SOLOVEY_TEST_PARENT_NETNS"), "LANG=C", "TZ=UTC0"}
		var output bytes.Buffer
		cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(data), &output, &output
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd, &output
	}
	cmd, output := run()
	var protectErr error
	for _, proto := range []int{unix.IPPROTO_TCP, unix.IPPROTO_UDP} {
		if protectErr = markZapretProbeSocket(ctx, attestor, listener, cmd.Process.Pid, proto, mark); protectErr != nil {
			_ = cmd.Process.Kill()
			break
		}
	}
	if err := cmd.Wait(); err != nil || protectErr != nil {
		t.Fatalf("capless Direct probe did not qualify: %v %v\n%s", err, protectErr, output)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	input.Unavailable = true
	cmd, output = run()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Direct did not fail closed without protect_path: %v\n%s", err, output)
	}
	t.Log("CAPLESS_DIRECT_PROVEN: the pinned Direct dialer used protected TCP/UDP sockets without Panel capabilities; missing helper blocked A")
	t.Log("CAPLESS_PEER_ATTESTED: fixture receiver reused the broker executable/cgroup, pidfd and writer checks before acknowledging the socket")
}

func markZapretProbeSocket(ctx context.Context, attestor broker.ManifestAttestor, listener *net.UnixListener, pid, protocol int, mark uint32) error {
	deadline := time.Now().Add(4 * time.Second)
	if err := listener.SetDeadline(deadline); err != nil {
		return err
	}
	conn, err := listener.AcceptUnix()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	peer, err := attestor.Attest(ctx, conn, broker.RolePanel)
	if err != nil {
		return fmt.Errorf("protect_path connector attestation: %w", err)
	}
	defer attestor.ClosePeer(peer)
	if peer.PID != pid {
		return errors.New("protect_path connector is not the launched peer")
	}
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var netns uint64
	var netnsErr error
	if err := raw.Control(func(fd uintptr) {
		netns, netnsErr = unix.GetsockoptUint64(int(fd), unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
	}); err != nil || netnsErr != nil || netns == 0 {
		return errors.New("protect_path connector namespace is unavailable")
	}
	var data [2]byte
	control := make([]byte, unix.CmsgSpace(unix.SizeofUcred)+unix.CmsgSpace(4*4))
	n, oobn, flags, _, err := conn.ReadMsgUnix(data[:], control)
	messages, parseErr := unix.ParseSocketControlMessage(control[:oobn])
	var sockets []int
	var writer *unix.Ucred
	for _, message := range messages {
		if credential, err := unix.ParseUnixCredentials(&message); err == nil {
			writer = credential
			continue
		}
		fds, err := unix.ParseUnixRights(&message)
		if err == nil {
			sockets = append(sockets, fds...)
			for _, fd := range fds {
				defer unix.Close(fd)
			}
		}
	}
	if err != nil || parseErr != nil || n != 1 || data[0] != 1 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 ||
		len(messages) != 2 || len(sockets) != 1 || writer == nil {
		return errors.New("protect_path did not send one socket")
	}
	if err := attestor.VerifyWriter(ctx, peer, broker.WriterCredentials{PID: int(writer.Pid), UID: writer.Uid, GID: writer.Gid}); err != nil {
		return err
	}
	fd := sockets[0]
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFSOCK || stat.Uid != zapretProbeUID {
		return errors.New("protect_path descriptor is not owned by the launched peer")
	}
	if actual, err := unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_NETNS_COOKIE); err != nil || actual != netns {
		return errors.New("protect_path socket is not in the approved namespace")
	}
	if domain, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_DOMAIN); err != nil || domain != unix.AF_INET && domain != unix.AF_INET6 {
		return errors.New("protect_path accepts only inet sockets")
	}
	if actual, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PROTOCOL); err != nil || actual != protocol {
		return errors.New("unexpected protect_path socket protocol")
	}
	if listener, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ACCEPTCONN); err != nil || listener != 0 {
		return errors.New("protect_path cannot mark a listener")
	}
	if _, err := unix.Getpeername(fd); !errors.Is(err, unix.ENOTCONN) {
		return errors.New("protect_path socket must not be connected")
	}
	if existing, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_MARK); err != nil || existing != 0 {
		return errors.New("protect_path would replace an existing mark")
	}
	if err := attestor.Recheck(ctx, peer, broker.RolePanel); err != nil {
		return err
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_MARK, int(mark)); err != nil {
		return err
	}
	_, err = conn.Write([]byte{1})
	return err
}

func TestZapretCaplessPeer(t *testing.T) {
	parent := os.Getenv("SOLOVEY_TEST_PROTECT_PEER")
	if parent == "" {
		t.Skip("child of namespace qualifier only")
	}
	namespace, nsErr := os.Readlink("/proc/self/ns/net")
	links, linkErr := net.Interfaces()
	if nsErr != nil || namespace == parent || linkErr != nil || len(links) != 1 || links[0].Name != "lo" || os.Geteuid() != zapretProbeUID || os.Getegid() != zapretProbeUID {
		t.Fatal("protect_path peer is not the capless isolated fixture")
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"Inh", "Prm", "Eff", "Bnd", "Amb"} {
		if !strings.Contains(string(status), "\nCap"+key+":\t0000000000000000\n") {
			t.Fatalf("nonempty Cap%s", key)
		}
	}
	if !strings.Contains(string(status), "\nNoNewPrivs:\t1\n") {
		t.Fatal("NoNewPrivileges not set")
	}
	var input zapretProtectInput
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 8192)).Decode(&input); err != nil {
		t.Fatal(err)
	}
	core := coreruntime.NewCore()
	if err := core.Start(input.Configuration); err != nil {
		t.Fatal(err)
	}
	defer core.Stop()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for _, destination := range []struct{ network, address string }{{"tcp", input.TCP}, {"udp", input.UDP}} {
		for _, tag := range []string{"direct-a", "direct-b"} {
			conn, err := core.DialOutbound(ctx, tag, destination.network, M.ParseSocksaddr(destination.address))
			if input.Unavailable && tag == "direct-a" {
				if conn != nil {
					_ = conn.Close()
				}
				if err == nil || !strings.Contains(err.Error(), "connect protect path") {
					t.Fatalf("missing protect_path was not the cause of the dial refusal: %v", err)
				}
				continue
			}
			zapretRoundTrip(t, conn, err)
		}
	}
}
