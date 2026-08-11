//go:build linux

package privilegedbroker

import (
	"errors"
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func verifyServerConnection(connection net.Conn, socketPath string) error {
	if socketPath != DefaultSocketPath && socketPath != ProofSocketPath {
		return errors.New("broker socket path is not fixed")
	}
	info, err := os.Lstat(socketPath)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o660 {
		return errors.New("broker socket ownership mode is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Nlink != 1 || os.Geteuid() != 0 && stat.Gid != uint32(os.Getegid()) {
		return errors.New("broker socket is not root-owned")
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		return errors.New("broker connection is not AF_UNIX")
	}
	raw, err := unixConnection.SyscallConn()
	if err != nil {
		return err
	}
	var credential *unix.Ucred
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		credential, socketErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if socketErr != nil || credential == nil || credential.Uid != 0 || credential.Pid <= 1 {
		return errors.New("broker server peer is not the root service")
	}
	return nil
}
