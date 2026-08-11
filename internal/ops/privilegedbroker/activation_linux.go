//go:build linux

package privilegedbroker

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
)

// ActivatedListeners consumes exactly the two systemd-owned AF_UNIX sockets.
// There is deliberately no path-listening fallback and no caller-controlled
// file descriptor or socket path.
func ActivatedListeners() (map[Role]*net.UnixListener, error) {
	pid, err := strconv.Atoi(os.Getenv("LISTEN_PID"))
	if err != nil {
		return nil, errors.New("privileged broker systemd socket activation PID is invalid")
	}
	names, err := activatedDescriptorNames(pid, os.Getpid(), os.Getenv("LISTEN_FDS"), os.Getenv("LISTEN_FDNAMES"))
	if err != nil {
		return nil, err
	}
	result := make(map[Role]*net.UnixListener, 2)
	for index, name := range names {
		role := RolePanel
		if name == "proof" {
			role = RoleSSHProof
		}
		file := os.NewFile(uintptr(3+index), names[index])
		if file == nil {
			return nil, fmt.Errorf("systemd socket descriptor %d is absent", 3+index)
		}
		listener, listenErr := net.FileListener(file)
		_ = file.Close()
		if listenErr != nil {
			return nil, listenErr
		}
		unixListener, ok := listener.(*net.UnixListener)
		if !ok {
			_ = listener.Close()
			return nil, errors.New("systemd activated broker descriptor is not AF_UNIX")
		}
		expectedPath := DefaultSocketPath
		if role == RoleSSHProof {
			expectedPath = ProofSocketPath
		}
		address, ok := unixListener.Addr().(*net.UnixAddr)
		if !ok || address.Net != "unix" || address.Name != expectedPath {
			_ = unixListener.Close()
			return nil, errors.New("systemd activated broker socket path is unexpected")
		}
		result[role] = unixListener
	}
	return result, nil
}
