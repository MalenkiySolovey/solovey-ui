package runtime

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

var errDiagnosticDatagramPeer = errors.New("diagnostic_datagram_peer_mismatch")

func dialDiagnosticDatagram(ctx context.Context, selected adapter.Outbound, target M.Socksaddr) (net.Conn, error) {
	// DialContext("udp") may hide the reply address or report the carrier
	// peer. Keep the native packet metadata until the exact peer is checked.
	packets, err := selected.ListenPacket(ctx, target)
	if err != nil {
		if packets != nil {
			_ = packets.Close()
		}
		return nil, err
	}
	if packets == nil {
		return nil, ErrDiagnosticDatagram
	}
	return &diagnosticDatagramConn{packets: bufio.NewPacketConn(packets), target: target}, nil
}

type diagnosticDatagramConn struct {
	packets         N.NetPacketConn
	target          M.Socksaddr
	readMu, writeMu sync.Mutex
}

func (c *diagnosticDatagramConn) RemoteAddr() net.Addr { return c.target.UDPAddr() }
func (c *diagnosticDatagramConn) LocalAddr() net.Addr  { return c.packets.LocalAddr() }
func (c *diagnosticDatagramConn) Close() error         { return c.packets.Close() }

func (c *diagnosticDatagramConn) SetDeadline(t time.Time) error { return c.packets.SetDeadline(t) }

func (c *diagnosticDatagramConn) SetReadDeadline(t time.Time) error {
	return c.packets.SetReadDeadline(t)
}

func (c *diagnosticDatagramConn) SetWriteDeadline(t time.Time) error {
	return c.packets.SetWriteDeadline(t)
}

func (c *diagnosticDatagramConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	// Packet encodings can read their wire address before peeling it off.
	buffer := buf.NewSize(min(len(p), 65535) + M.MaxSocksaddrLength)
	defer buffer.Release()
	peer, err := c.packets.ReadPacket(buffer)
	if err != nil {
		return 0, err
	}
	if peer.Unwrap() != c.target {
		return 0, errDiagnosticDatagramPeer
	}
	if buffer.Len() > len(p) {
		return 0, io.ErrShortBuffer
	}
	return copy(p, buffer.Bytes()), nil
}

func (c *diagnosticDatagramConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	// The provider accounts for frame headers; expose only payload bytes.
	return bufio.WritePacketBuffer(c.packets, buf.As(p), c.target)
}
