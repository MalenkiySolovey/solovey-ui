//go:build linux

package privilegedbroker

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
)

// ActivatedListeners consumes the two systemd-owned AF_UNIX sockets. A full
// build also verifies and closes its extra copy of a deny port held by PID 1.
// There is deliberately no path-listening fallback and no caller-controlled
// file descriptor or socket path.
func ActivatedListeners() (_ map[Role]*net.UnixListener, _ zapretDenyIdentity, failure error) {
	pid, err := strconv.Atoi(os.Getenv("LISTEN_PID"))
	if err != nil {
		return nil, zapretDenyIdentity{}, errors.New("privileged broker systemd socket activation PID is invalid")
	}
	names, err := activatedDescriptorNames(pid, os.Getpid(), os.Getenv("LISTEN_FDS"), os.Getenv("LISTEN_FDNAMES"))
	if err != nil {
		return nil, zapretDenyIdentity{}, err
	}
	result := make(map[Role]*net.UnixListener, 2)
	var deny zapretDenyIdentity
	defer func() {
		if failure != nil {
			for _, listener := range result {
				_ = listener.Close()
			}
		}
	}()
	for index, name := range names {
		fd := 3 + index
		if name == zapretDenyFDName {
			deny, err = closeInheritedZapretDeny(fd)
			if err != nil {
				return nil, zapretDenyIdentity{}, err
			}
			continue
		}
		role := RolePanel
		if name == "proof" {
			role = RoleSSHProof
		}
		file := os.NewFile(uintptr(fd), name)
		if file == nil {
			return nil, zapretDenyIdentity{}, fmt.Errorf("systemd socket descriptor %d is absent", fd)
		}
		listener, listenErr := net.FileListener(file)
		_ = file.Close()
		if listenErr != nil {
			return nil, zapretDenyIdentity{}, listenErr
		}
		unixListener, ok := listener.(*net.UnixListener)
		if !ok {
			_ = listener.Close()
			return nil, zapretDenyIdentity{}, errors.New("systemd activated broker descriptor is not AF_UNIX")
		}
		expectedPath := DefaultSocketPath
		if role == RoleSSHProof {
			expectedPath = ProofSocketPath
		}
		address, ok := unixListener.Addr().(*net.UnixAddr)
		if !ok || address.Net != "unix" || address.Name != expectedPath {
			_ = unixListener.Close()
			return nil, zapretDenyIdentity{}, errors.New("systemd activated broker socket path is unexpected")
		}
		result[role] = unixListener
	}
	return result, deny, nil
}
