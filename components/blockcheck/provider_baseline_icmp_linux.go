//go:build linux && !minimal

package blockcheck

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net/netip"

	feature "github.com/MalenkiySolovey/solovey-component-blockcheck"
	"github.com/mdlayher/socket"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/sys/unix"
)

func (b hostBaseline) echoICMP(ctx context.Context, addr netip.Addr) (feature.SocketEvidence, error) {
	if !permitTarget(addr) || !addr.IsGlobalUnicast() || addr.Zone() != "" || addr.Is4In6() {
		return feature.SocketEvidence{}, feature.ErrTargetRejected
	}
	handle, before, err := beginBaselineRoute(ctx, addr)
	if err != nil {
		return feature.SocketEvidence{}, err
	}
	defer handle.Close()
	probe := echoEndpoint(addr)
	conn, err := probe.open(ctx)
	if err != nil {
		return feature.SocketEvidence{}, err
	}
	defer conn.Close()
	raw, err := conn.SyscallConn()
	if err != nil || b.inspectSocket(ctx, "", "", raw) != nil {
		return feature.SocketEvidence{}, feature.ErrBaselineUnobservable
	}
	sockaddr, err := conn.Getsockname()
	local, id := echoAddress(sockaddr)
	if err != nil || id == 0 || local != before.source {
		return feature.SocketEvidence{}, feature.ErrBaselineUnobservable
	}
	after, err := baselineLookup(ctx, handle, addr, local)
	if err != nil || after != before || ctx.Err() != nil {
		return feature.SocketEvidence{}, feature.ErrBaselineUnobservable
	}
	if err := probe.exchange(ctx, conn, addr, id); errors.Is(err, feature.ErrICMPEchoNotSent) {
		return feature.SocketEvidence{}, err
	} else {
		return feature.SocketEvidence{Interface: after.name, OutsidePanel: true}, err
	}
}

type hostEcho struct {
	domain, protocol int
	peer             unix.Sockaddr
	request, reply   icmp.Type
}

func echoEndpoint(addr netip.Addr) hostEcho {
	if addr.Is4() {
		return hostEcho{unix.AF_INET, unix.IPPROTO_ICMP, &unix.SockaddrInet4{Addr: addr.As4()}, ipv4.ICMPTypeEcho, ipv4.ICMPTypeEchoReply}
	}
	return hostEcho{unix.AF_INET6, unix.IPPROTO_ICMPV6, &unix.SockaddrInet6{Addr: addr.As16()}, ipv6.ICMPTypeEchoRequest, ipv6.ICMPTypeEchoReply}
}

func echoAddress(sa unix.Sockaddr) (netip.Addr, int) {
	switch sa := sa.(type) {
	case *unix.SockaddrInet4:
		return netip.AddrFrom4(sa.Addr), sa.Port
	case *unix.SockaddrInet6:
		if sa.ZoneId == 0 {
			return netip.AddrFrom16(sa.Addr), sa.Port
		}
	}
	return netip.Addr{}, 0
}

func (p hostEcho) open(ctx context.Context) (*socket.Conn, error) {
	// Linux checks ping_group_range for these datagram sockets. Never fall
	// back to raw sockets or modify the Host's permission settings.
	conn, err := socket.Socket(p.domain, unix.SOCK_DGRAM, p.protocol, "blockcheck-icmp", nil)
	if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) || errors.Is(err, unix.EAFNOSUPPORT) || errors.Is(err, unix.EPROTONOSUPPORT) {
		return nil, feature.ErrICMPEchoUnavailable
	}
	if err != nil {
		return nil, feature.ErrICMPEchoNotSent
	}
	if _, err := conn.Connect(ctx, p.peer); err != nil {
		_ = conn.Close()
		return nil, feature.ErrICMPEchoNotSent
	}
	return conn, nil
}

const echoSeq = 1

func (p hostEcho) exchange(ctx context.Context, conn *socket.Conn, dst netip.Addr, id int) error {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return feature.ErrICMPEchoNotSent
	}
	message := icmp.Message{Type: p.request, Body: &icmp.Echo{ID: id, Seq: echoSeq, Data: nonce[:]}}
	data, err := message.Marshal(nil)
	if err != nil {
		return feature.ErrICMPEchoNotSent
	}
	// Echo has no destination port, so Linux getpeername reports ENOTCONN
	// even after a successful connect. Send without a per-message address to
	// the connected peer and verify the source on receipt.
	if n, err := conn.Sendmsg(ctx, data, nil, nil, 0); err != nil || n != len(data) {
		return feature.ErrICMPEchoNotSent
	}
	n, _, flags, sa, err := conn.Recvmsg(ctx, data, nil, 0)
	if err != nil {
		return err
	}
	peer, _ := echoAddress(sa)
	if flags&unix.MSG_TRUNC != 0 || n != len(data) || peer != dst || !p.matchesReply(data, id, nonce[:]) {
		return errors.New("ICMP Echo reply did not match the request")
	}
	return nil
}

func (p hostEcho) matchesReply(data []byte, id int, nonce []byte) bool {
	message, err := icmp.ParseMessage(p.protocol, data)
	if err != nil || message.Type != p.reply || message.Code != 0 {
		return false
	}
	echo, ok := message.Body.(*icmp.Echo)
	return ok && echo.ID == id && echo.Seq == echoSeq && bytes.Equal(echo.Data, nonce)
}
