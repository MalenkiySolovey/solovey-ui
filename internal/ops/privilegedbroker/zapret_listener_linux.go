//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"golang.org/x/sys/unix"
)

const zapretSocketRoot = zapretactivation.SocketRoot

func zapretSocketPath(root, generation string) string {
	return filepath.Join(root, "z-"+generation)
}

func bindZapretSocketBatch(ctx context.Context, root string, gid uint32, grants []ZapretSocketGrant) ([]*standaloneSocket, error) {
	endpoints := make([]*standaloneSocket, 0, len(grants))
	for i, grant := range grants {
		endpoint, err := bindZapretSocket(ctx, root, gid, grant.Generation)
		if err != nil {
			err = fmt.Errorf("cannot publish Zapret socket %d: %w", i+1, err)
			for _, bound := range endpoints {
				err = errors.Join(err, bound.close())
			}
			return nil, err
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, nil
}

// bindZapretSocket only creates a fresh endpoint. A previous Direct config
// may still refer to an existing path, including a path with no live owner.
func bindZapretSocket(ctx context.Context, root string, gid uint32, generation string) (*standaloneSocket, error) {
	if gid == 0 || !digestPattern.MatchString(generation) || !canonicalStandaloneRoot(root) {
		return nil, errZapretSocket
	}
	if err := verifyStandaloneDirectory(root, gid); err != nil {
		return nil, err
	}
	for dir := filepath.Dir(root); ; dir = filepath.Dir(dir) {
		var st unix.Stat_t
		if err := unix.Lstat(dir, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Mode&0o022 != 0 {
			return nil, errors.New("socket directory for Zapret has unsafe ancestry")
		}
		if dir == "/" {
			break
		}
	}
	path := zapretSocketPath(root, generation)
	if len(path) >= len(unix.RawSockaddrUnix{}.Path) {
		return nil, errors.New("socket path for Zapret exceeds the Unix address limit")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("socket generation path for Zapret is not vacant")
	}
	config := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		// A client may write immediately after connect, before AcceptUnix.
		var err error
		controlErr := raw.Control(func(fd uintptr) {
			err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_PASSCRED, 1)
		})
		return errors.Join(controlErr, err)
	}}
	listener, err := config.Listen(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("bind Zapret admission socket: %w", err)
	}
	return claimBoundBrokerSocket(RolePanel, listener.(*net.UnixListener), path, gid)
}
