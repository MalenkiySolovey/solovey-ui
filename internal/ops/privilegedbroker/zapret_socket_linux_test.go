package privilegedbroker

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-host/hostref"
	"github.com/MalenkiySolovey/solovey-host/network/intent"
	platform "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"golang.org/x/sys/unix"
)

func TestZapretSocketWireIsOneRequestOneDescriptor(t *testing.T) {
	for _, tc := range []struct {
		name  string
		wire  []byte
		count int
		valid bool
	}{
		{"one", []byte{1}, 1, true},
		{"unknown byte", []byte{2}, 1, false},
		{"extra data", []byte{1, 1}, 1, false},
		{"no descriptor", []byte{1}, 0, false},
		{"two descriptors", []byte{1}, 2, false},
		{"truncated control", []byte{1}, 24, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Use a short directory so the generated path fits sockaddr_un.
			dir, err := os.MkdirTemp("/tmp", "sui-z-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Remove(dir) })
			addr := &net.UnixAddr{Name: filepath.Join(dir, "socket"), Net: "unix"}
			listener, err := net.ListenUnix("unix", addr)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			if err := preparePeerListener(listener); err != nil {
				t.Fatal(err)
			}
			client, err := net.DialUnix("unix", nil, addr)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			server, err := listener.AcceptUnix()
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			_ = server.SetReadDeadline(time.Now().Add(time.Second))
			socket, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, unix.IPPROTO_TCP)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(socket)
			var rights []byte
			if tc.count != 0 {
				fds := make([]int, tc.count)
				for i := range fds {
					fds[i] = socket
				}
				rights = unix.UnixRights(fds...)
			}
			if _, _, err := client.WriteMsgUnix(tc.wire, rights, nil); err != nil {
				t.Fatal(err)
			}
			writer, fd, err := readZapretSocket(server)
			if fd >= 0 {
				defer unix.Close(fd)
			}
			if !tc.valid {
				if err == nil || fd != -1 {
					t.Fatal("accepted an invalid wire request")
				}
				return
			}
			if err != nil || writer.PID != os.Getpid() || writer.UID != uint32(os.Geteuid()) || writer.GID != uint32(os.Getegid()) {
				t.Fatalf("writer was not taken from SCM_CREDENTIALS: %#v (%v)", writer, err)
			}
			if flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil || flags&unix.FD_CLOEXEC == 0 {
				t.Fatalf("received descriptor was not close-on-exec: %x (%v)", flags, err)
			}
		})
	}
}

func TestZapretSocketScopeOnlyUnboundOwnedTCP(t *testing.T) {
	const ipv4 = unix.AF_INET
	open := func(kind int) int {
		t.Helper()
		fd, err := unix.Socket(ipv4, kind|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = unix.Close(fd) })
		return fd
	}
	tcp := open(unix.SOCK_STREAM)
	cookie, err := socketNetNSCookie(tcp)
	if err != nil {
		t.Fatal(err)
	}
	peer := PeerIdentity{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
	scope := testZapretSocketScope(t, cookie)
	if err := checkZapretSocket(tcp, peer, scope); err != nil {
		t.Fatalf("an unbound owner-created TCP socket was refused: %v", err)
	}
	if err := checkZapretSocket(tcp, peer, testZapretSocketScope(t, cookie+1)); err == nil {
		t.Fatal("accepted another namespace cookie")
	}
	if err := checkZapretSocket(open(unix.SOCK_DGRAM), peer, scope); err == nil {
		t.Fatal("accepted UDP outside the strategy's scope")
	}
	foreign := peer
	foreign.UID++
	if err := checkZapretSocket(tcp, foreign, scope); err == nil {
		t.Fatal("accepted a socket owned by a different UID")
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := unix.Connect(tcp, &unix.SockaddrInet4{Port: listener.Addr().(*net.TCPAddr).Port, Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		t.Fatal(err)
	}
	if err := checkZapretSocket(tcp, peer, scope); err == nil {
		t.Fatal("accepted a connected TCP socket")
	}
}

func testZapretSocketScope(t *testing.T, cookie uint64) planner.ZapretSocketScope {
	t.Helper()
	revision := strings.Repeat("a", 64)
	plan, err := planner.CompileZapret(intent.ZapretPacketPathIntent{
		Schema: intent.ZapretPacketPathSchema, HostRef: hostref.Local(), Owner: intent.ZapretOwner,
		ResourceRef: "outbound:direct-a", OutboundRef: "direct-a", StrategyRef: "test", Purpose: intent.ZapretPurpose,
		StrategyRevision: revision, InputRevision: revision, DesiredRevision: revision,
		Protocols: []intent.PacketProtocol{intent.PacketProtocolTCP}, TCPPorts: []uint16{443},
		Directions: []intent.PacketDirection{intent.PacketDirectionOutbound},
	}, planner.Facts{
		HostRef: hostref.Local(), Revision: revision, CapabilityRevision: revision, InventoryComplete: true,
		Capability: platform.PlatformFact{Availability: platform.Available, Backend: planner.ZapretInstalledBackend},
		Zapret: &planner.ZapretFacts{
			OutboundRef: "direct-a", OutboundRevision: revision, Provider: "sing-box",
			DirectSocket: true, SocketMark: true, NFQueue: true, ConntrackAccounting: true, EgressGuard: true,
			InstalledEgress: &planner.ZapretEgress{NamespaceCookie: cookie, IfIndex: 2, IPv4: true, RouteRevision: revision},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := plan.SocketScope()
	if err != nil {
		t.Fatal(err)
	}
	return scope
}
