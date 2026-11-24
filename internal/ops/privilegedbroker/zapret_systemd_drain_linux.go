//go:build linux && !minimal

package privilegedbroker

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"syscall"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
	"github.com/mdlayher/netlink"
	"github.com/mdlayher/socket"
	"golang.org/x/sys/unix"
)

// Admission has stopped before this runs. The systemd profile gives marked
// sockets only to the attested Panel process. Its descriptor table includes
// sockets still between protect_path and connect. The kernel TCP inventory
// below also includes sockets that outlived their final userspace descriptor.
func proveZapretDrain(ctx context.Context, peer PeerIdentity) error {
	if err := peerAlive(peer); err != nil {
		return fmt.Errorf("cannot prove the retiring Panel socket inventory: %w", err)
	}
	snapshot, err := processevidence.ObserveDescriptorSnapshot(peer.PID)
	if err != nil {
		return err
	}
	for fd, identity := range snapshot.SocketInodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		inode, err := strconv.ParseUint(identity, 10, 64)
		if err != nil {
			return err
		}
		duplicate, err := unix.PidfdGetfd(peer.livenessFD, fd, 0)
		if errors.Is(err, unix.EBADF) {
			continue // The original descriptor closed after the snapshot.
		}
		if err != nil {
			return err
		}
		var stat unix.Stat_t
		statErr := unix.Fstat(duplicate, &stat)
		mark, markErr := unix.GetsockoptInt(duplicate, unix.SOL_SOCKET, unix.SO_MARK)
		_ = unix.Close(duplicate)
		if statErr == nil && stat.Ino != inode {
			continue // No new admission can occur while the mutation fence is held.
		}
		if statErr != nil || markErr != nil {
			return errors.New("cannot verify retirement of a Panel socket")
		}
		if uint32(mark)&planner.ZapretDenyMask != 0 {
			return errors.New("the Panel still holds a marked Zapret socket")
		}
	}
	if err := peerAlive(peer); err != nil {
		return fmt.Errorf("the retiring Panel identity changed during the socket inventory: %w", err)
	}
	return retireZapretTCPInKernel(ctx)
}

const (
	sockDiagByFamily    = 20 // linux/sock_diag.h
	inetDiagReqBytecode = 1  // linux/inet_diag.h
	inetDiagBCMarkCond  = 10
	inetDiagMark        = 15
	tcpTimeWait         = 6
	tcpClose            = 7
)

// An orphaned TCP socket can keep retransmitting after the Core has closed
// its descriptor. Abort only orphans bearing the exclusively reserved bit
// and the exact kernel cookie, while the deny still holds.
func retireZapretTCPInKernel(ctx context.Context) error {
	if err := retireZapretTCPFamily(ctx, unix.AF_INET); err != nil {
		return err
	}
	err := retireZapretTCPFamily(ctx, unix.AF_INET6)
	if errors.Is(err, unix.EAFNOSUPPORT) {
		// Skip the inventory only if the kernel cannot create IPv6 sockets.
		fd, probeErr := unix.Socket(unix.AF_INET6, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, unix.IPPROTO_TCP)
		if errors.Is(probeErr, unix.EAFNOSUPPORT) {
			return nil
		}
		if probeErr == nil {
			_ = unix.Close(fd)
		}
	}
	return err
}

func retireZapretTCPFamily(ctx context.Context, family byte) error {
	conn, err := socket.Socket(unix.AF_NETLINK, unix.SOCK_RAW, unix.NETLINK_SOCK_DIAG, "zapret-drain", nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.Bind(&unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return err
	}
	request := make([]byte, 56) // struct inet_diag_req_v2
	request[0], request[1] = family, unix.IPPROTO_TCP
	binary.NativeEndian.PutUint32(request[4:], ^uint32(1<<tcpTimeWait|1<<tcpClose))
	mark := planner.ZapretSocketMarkBit
	filter := []byte{inetDiagBCMarkCond, 12, 0, 0}
	binary.NativeEndian.PutUint16(filter[2:], 16)
	filter = binary.NativeEndian.AppendUint32(filter, mark)
	filter = binary.NativeEndian.AppendUint32(filter, mark)
	attrs, err := netlink.MarshalAttributes([]netlink.Attribute{{Type: inetDiagReqBytecode, Data: filter}})
	if err != nil {
		return err
	}
	dump := append(request, attrs...)
	replies, err := zapretSockDiag(ctx, conn, sockDiagByFamily, netlink.Dump, 1, dump)
	if err != nil {
		return fmt.Errorf("list draining Zapret sockets: %w", err)
	}
	for _, entry := range replies {
		id, err := retiredZapretTCPID(entry, family)
		if err != nil {
			return err
		}
		copy(request[8:], id)
		if _, err := zapretSockDiag(ctx, conn, unix.SOCK_DESTROY, netlink.Acknowledge, 2, request); err != nil && !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("retire buffered Zapret TCP: %w", err)
		}
	}
	if len(replies) != 0 {
		remaining, err := zapretSockDiag(ctx, conn, sockDiagByFamily, netlink.Dump, 3, dump)
		if err != nil || len(remaining) != 0 {
			return errors.Join(errors.New("marked Zapret TCP sockets may still transmit"), err)
		}
	}
	return ctx.Err()
}

func retiredZapretTCPID(entry []byte, family byte) ([]byte, error) {
	// sock_i_uid and sock_i_ino report zero when the TCP socket has lost
	// its userspace owner. Only the broker can issue the reserved mark.
	if len(entry) < 72 || entry[0] != family || binary.NativeEndian.Uint32(entry[64:68]) != 0 ||
		binary.NativeEndian.Uint32(entry[68:72]) != 0 {
		return nil, errors.New("a marked Zapret socket still has a userspace owner")
	}
	if cookie := binary.NativeEndian.Uint64(entry[44:52]); cookie == 0 || cookie == ^uint64(0) {
		return nil, errors.New("kernel did not identify the retired Zapret socket cookie")
	}
	decoder, err := netlink.NewAttributeDecoder(entry[72:])
	if err != nil {
		return nil, err
	}
	var mark uint32
	for decoder.Next() {
		if decoder.Type() == inetDiagMark {
			mark = decoder.Uint32()
		}
	}
	if decoder.Err() != nil || mark&^(planner.ZapretMaxSerial|planner.ZapretReturnSocketBit) != planner.ZapretSocketMarkBit || mark&planner.ZapretMaxSerial == 0 {
		return nil, errors.New("kernel did not identify the retired Zapret socket mark")
	}
	return entry[4:52], nil // inet_diag_sockid (includes the kernel cookie)
}

func zapretSockDiag(ctx context.Context, conn *socket.Conn, operation netlink.HeaderType, flags netlink.HeaderFlags, sequence uint32, payload []byte) ([][]byte, error) {
	msg := netlink.Message{Header: netlink.Header{
		Length: uint32(unix.SizeofNlMsghdr + len(payload)), Type: operation, Flags: netlink.Request | flags, Sequence: sequence,
	}, Data: payload}
	data, err := msg.MarshalBinary()
	if err != nil {
		return nil, err
	}
	if err := conn.Sendto(ctx, data, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil, err
	}
	var packet [8192]byte
	var entries [][]byte
	for {
		n, _, received, from, err := conn.Recvmsg(ctx, packet[:], nil, 0)
		if err != nil {
			return nil, err
		}
		peer, ok := from.(*unix.SockaddrNetlink)
		if !ok || peer.Pid != 0 || received&unix.MSG_TRUNC != 0 {
			return nil, errors.New("unexpected Zapret socket inventory sender or length")
		}
		replies, err := syscall.ParseNetlinkMessage(packet[:n])
		if err != nil || len(replies) == 0 {
			return nil, errors.New("invalid Zapret socket inventory reply")
		}
		for _, reply := range replies {
			if reply.Header.Seq != sequence || reply.Header.Flags&unix.NLM_F_DUMP_INTR != 0 {
				return nil, errors.New("interrupted Zapret socket inventory")
			}
			switch reply.Header.Type {
			case unix.NLMSG_DONE, unix.NLMSG_ERROR:
				if len(reply.Data) < 4 {
					return nil, errors.New("missing Zapret socket inventory status")
				}
				status := int32(binary.NativeEndian.Uint32(reply.Data))
				if status < 0 {
					return nil, unix.Errno(-status)
				}
				if status != 0 || (flags == netlink.Dump) != (reply.Header.Type == unix.NLMSG_DONE) {
					return nil, errors.New("unexpected Zapret socket inventory status")
				}
				return entries, nil
			case sockDiagByFamily:
				if flags != netlink.Dump || len(entries) >= 4096 {
					return nil, errors.New("unexpected or excessive Zapret socket inventory")
				}
				entries = append(entries, append([]byte(nil), reply.Data...))
			default:
				return nil, errors.New("unexpected Zapret socket inventory message")
			}
		}
	}
}
