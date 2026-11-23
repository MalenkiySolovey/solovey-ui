//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"errors"
	"net"
	"os"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"golang.org/x/sys/unix"
)

var errZapretTUN = errors.New("installed Zapret TUN handoff denied")

// ReceiveZapretTUN requests one attached TUN from the root-owned, live batch
// endpoint. This protocol is separate from the ordinary broker transport.
func ReceiveZapretTUN(ctx context.Context, batch string) (*os.File, error) {
	path, ok := zapretactivation.TUNSocketPath(batch)
	if !ok {
		return nil, errZapretTUN
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	dialer := net.Dialer{}
	raw, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, errors.Join(errZapretTUN, err)
	}
	conn := raw.(*net.UnixConn)
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if err := preparePeerConnection(conn); err != nil {
		return nil, err
	}
	sock, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var peer *unix.Ucred
	var peerErr error
	if err := sock.Control(func(fd uintptr) { peer, peerErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil || peerErr != nil || peer == nil || peer.Uid != 0 || peer.Pid <= 1 {
		return nil, errZapretTUN
	}
	if n, err := conn.Write([]byte{1}); err != nil || n != 1 {
		return nil, errors.Join(errZapretTUN, err)
	}
	writer, fd, err := readZapretSocket(conn)
	if err != nil {
		return nil, errZapretTUN
	}
	if writer != (WriterCredentials{PID: int(peer.Pid), UID: peer.Uid, GID: peer.Gid}) || ctx.Err() != nil {
		_ = unix.Close(fd)
		return nil, errZapretTUN
	}
	return os.NewFile(uintptr(fd), "admitted-zapret-tun"), nil
}

func (s *Server) serveZapretTUN(ctx context.Context, endpoint *standaloneSocket, expected PeerIdentity, file *os.File,
	verify func(context.Context) error,
) error {
	defer endpoint.close()
	stop := context.AfterFunc(ctx, func() { _ = endpoint.listener.Close() })
	defer stop()
	for ctx.Err() == nil {
		conn, err := endpoint.listener.AcceptUnix()
		if err != nil {
			return err
		}
		delivered := s.sendZapretTUN(ctx, conn, expected, file, verify)
		_ = conn.Close()
		if delivered {
			return nil // A single FD transfer is permitted per Core generation.
		}
	}
	return ctx.Err()
}

func (s *Server) sendZapretTUN(ctx context.Context, conn *net.UnixConn, expected PeerIdentity, file *os.File, verify func(context.Context) error) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	lease, err := s.beginAuthority(ctx)
	if err != nil {
		return false
	}
	defer lease.Release()
	attestor, ok := lease.Attestor.(ManifestAttestor)
	if !ok || !lease.Revisioned || lease.Revision != expected.ManifestRevision {
		return false
	}
	stop := context.AfterFunc(lease.Context, func() { _ = conn.Close() })
	defer stop()
	peer, err := attestor.Attest(lease.Context, conn, RolePanel)
	if err != nil {
		return false
	}
	defer attestor.ClosePeer(peer)
	if peer.BootID != s.BootID || peer.Revision != expected.Revision || peer.ManifestRevision != lease.Revision ||
		peer.UID == 0 || peer.CapabilitiesOnly || peer.Supervisor != "systemd" || peer.CgroupPolicy != CgroupRequired {
		return false
	}
	writer, err := readTUNClaim(conn)
	if err != nil || attestor.VerifyWriter(lease.Context, peer, writer) != nil || verify(lease.Context) != nil ||
		attestor.Recheck(lease.Context, peer, RolePanel) != nil {
		return false
	}
	s.authorityMu.Lock()
	defer s.authorityMu.Unlock()
	if s.retiring || s.authorityRevision != lease.Revision || lease.Context.Err() != nil {
		return false
	}
	// A write can transfer the descriptor even when its result is ambiguous.
	// Consume the one-shot endpoint after the first attempt, never replay it.
	_, _, _ = conn.WriteMsgUnix([]byte{1}, unix.UnixRights(int(file.Fd())), nil)
	return true
}

func readTUNClaim(conn *net.UnixConn) (WriterCredentials, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return WriterCredentials{}, err
	}
	var payload [2]byte
	control := make([]byte, unix.CmsgSpace(unix.SizeofUcred)+unix.CmsgSpace(4*4))
	var n, oob, flags int
	var recvErr error
	if err := raw.Read(func(fd uintptr) bool {
		n, oob, flags, _, recvErr = unix.Recvmsg(int(fd), payload[:], control, unix.MSG_CMSG_CLOEXEC)
		return !errors.Is(recvErr, unix.EAGAIN) && !errors.Is(recvErr, unix.EWOULDBLOCK)
	}); err != nil || recvErr != nil {
		return WriterCredentials{}, errZapretTUN
	}
	writer, err := receivedRequestCredentials(n, flags, control[:oob], false)
	if err != nil || n != 1 || payload[0] != 1 {
		return WriterCredentials{}, errZapretTUN
	}
	return writer, nil
}
