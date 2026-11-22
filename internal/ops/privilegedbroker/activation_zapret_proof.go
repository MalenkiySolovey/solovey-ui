//go:build linux && !minimal && zapretproof

package privilegedbroker

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

const zapretDenyActivationEnabled = true

func closeInheritedZapretDeny(fd int) error {
	return checkAndCloseZapretDeny(fd, systemdZapretFDStore)
}

func checkAndCloseZapretDeny(fd int, dump func(context.Context) ([]systemdStoredFD, error)) error {
	unix.CloseOnExec(fd)
	defer unix.Close(fd)
	address, addrErr := unix.Getsockname(fd)
	bound, ok := address.(*unix.SockaddrNetlink)
	protocol, protoErr := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PROTOCOL)
	typ, typeErr := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	var stat unix.Stat_t
	if addrErr != nil || !ok || bound.Pid == 0 || bound.Groups != 0 || protoErr != nil ||
		protocol != unix.NETLINK_NETFILTER || typeErr != nil || typ != unix.SOCK_RAW ||
		unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFSOCK || stat.Ino == 0 {
		return errors.New("stored Zapret deny descriptor is not a private netfilter owner port")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	entries, err := dump(ctx)
	// This unit has one store slot. A name or a successful notify alone
	// cannot show that PID 1 holds this same socket open.
	if err != nil || len(entries) != 1 || !entries[0].matchesZapretDeny(stat) {
		return errors.New("systemd did not confirm retention of the exact Zapret deny descriptor")
	}
	// No table ownership or grant is adopted. Only the duplicate closes.
	return nil
}

// systemd v255 DumpUnitFileDescriptorStore has response type a(suuutuusu).
type systemdStoredFD struct {
	Name                     string
	Mode, DevMajor, DevMinor uint32
	Inode                    uint64
	RDevMajor, RDevMinor     uint32
	Path                     string
	Flags                    uint32
}

func (s systemdStoredFD) matchesZapretDeny(stat unix.Stat_t) bool {
	return s.Name == zapretDenyFDName && s.Mode == stat.Mode && s.Inode == stat.Ino &&
		s.DevMajor == unix.Major(stat.Dev) && s.DevMinor == unix.Minor(stat.Dev)
}

func systemdZapretFDStore(ctx context.Context) ([]systemdStoredFD, error) {
	// Read only from PID 1's fixed private socket. No ambient bus address,
	// external command, or Unix FD negotiation.
	var dialer net.Dialer
	raw, err := dialer.DialContext(ctx, "unix", "/run/systemd/private")
	if err != nil {
		return nil, err
	}
	defer raw.Close()
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, errors.New("systemd file descriptor store read requires a deadline")
	}
	if err := raw.SetDeadline(deadline); err != nil {
		return nil, err
	}
	conn := raw.(*net.UnixConn)
	socket, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var peer *unix.Ucred
	var peerErr error
	if err := socket.Control(func(fd uintptr) {
		peer, peerErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil || peerErr != nil || peer == nil || peer.Pid != 1 || peer.Uid != 0 {
		return nil, errors.New("systemd file descriptor store is not served by PID 1")
	}
	bus, err := dbus.NewConn(conn, dbus.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer bus.Close()
	if err := bus.Auth([]dbus.Auth{dbus.AuthExternal(strconv.Itoa(unix.Geteuid()))}); err != nil {
		return nil, err
	}
	const manager = "org.freedesktop.systemd1"
	var entries []systemdStoredFD
	call := bus.Object(manager, "/org/freedesktop/systemd1").CallWithContext(ctx,
		manager+".Manager.DumpUnitFileDescriptorStore", 0, "solovey-privileged-broker.service")
	if err := call.Store(&entries); err != nil {
		return nil, err
	}
	return entries, nil
}
