package tracker

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/trafficcontrol"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"
)

// ConnTracker delegates live identities, metadata and closers to the one
// official inventory. It owns only Solovey terminal-I/O and reset fencing.
type ConnTracker struct {
	access    sync.Mutex
	inventory *trafficcontrol.Manager
	inflight  *trackerWaitGroup
	epoch     uint64
	closed    bool
}

func NewConnTracker(inventory *trafficcontrol.Manager) *ConnTracker {
	return &ConnTracker{inventory: inventory, inflight: newTrackerWaitGroup()}
}

func (c *ConnTracker) Reset() {
	c.access.Lock()
	connections := c.inventory.Connections()
	waitGroup := c.inflight
	c.inflight = newTrackerWaitGroup()
	c.epoch++
	c.access.Unlock()
	for _, metadata := range connections {
		if flow := c.inventory.Connection(metadata.ID); flow != nil {
			_ = flow.Close()
		}
	}
	waitForTrackerIdle("connection tracker", waitGroup, trackerResetWaitTimeout)
}

// Close permanently retires this generation. Reset alone allows new flows in
// the same generation; runtime shutdown must also reject late route callbacks.
func (c *ConnTracker) Close() {
	c.access.Lock()
	c.closed = true
	c.access.Unlock()
	c.Reset()
}

func (c *ConnTracker) RoutedConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, rule adapter.Rule, outbound adapter.Outbound) net.Conn {
	c.access.Lock()
	defer c.access.Unlock()
	if c.closed {
		_ = conn.Close()
		return conn
	}
	c.inflight.Add()
	tracking := &connectionTracking{waitGroup: c.inflight, ready: make(chan struct{})}
	wrapped := &wrappedConn{Conn: conn, tracking: tracking}
	actual := c.inventory.RoutedConnection(ctx, wrapped, metadata, rule, outbound)
	tracking.actual = actual
	close(tracking.ready)
	return actual
}

func (c *ConnTracker) RoutedPacketConnection(ctx context.Context, conn network.PacketConn, metadata adapter.InboundContext, rule adapter.Rule, outbound adapter.Outbound) network.PacketConn {
	c.access.Lock()
	defer c.access.Unlock()
	if c.closed {
		_ = conn.Close()
		return conn
	}
	c.inflight.Add()
	tracking := &connectionTracking{waitGroup: c.inflight, ready: make(chan struct{})}
	wrapped := &wrappedPacketConn{PacketConn: conn, tracking: tracking}
	actual := c.inventory.RoutedPacketConnection(ctx, wrapped, metadata, rule, outbound)
	tracking.actual = actual
	close(tracking.ready)
	return actual
}

func (c *ConnTracker) CloseConnByInbound(inbound string) int {
	c.access.Lock()
	connections := c.inventory.Connections()
	c.access.Unlock()
	closed := 0
	for _, metadata := range connections {
		if metadata.Metadata.Inbound == inbound {
			if flow := c.inventory.Connection(metadata.ID); flow != nil {
				_ = flow.Close()
				closed++
			}
		}
	}
	return closed
}

func shouldUntrackIOErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return !networkError.Timeout()
	}
	return true
}

type connectionTracking struct {
	actual    io.Closer
	waitGroup *trackerWaitGroup
	finished  atomic.Bool
	ready     chan struct{}
}

func (t *connectionTracking) done() {
	// The official closer calls the terminal wrapper again. CAS marks this
	// boundary before delegation, so recursive Close cannot enter it twice.
	if t.finished.CompareAndSwap(false, true) {
		// The official manager publishes its tracker before RoutedConnection
		// returns. A closer obtained from that inventory waits for binding.
		<-t.ready
		_ = t.actual.Close()
		t.waitGroup.Done()
	}
}

type wrappedConn struct {
	net.Conn
	tracking  *connectionTracking
	closeOnce sync.Once
	closeErr  error
}

func (w *wrappedConn) Read(b []byte) (int, error) {
	n, err := w.Conn.Read(b)
	if shouldUntrackIOErr(err) {
		w.tracking.done()
	}
	return n, err
}

func (w *wrappedConn) Write(b []byte) (int, error) {
	n, err := w.Conn.Write(b)
	if shouldUntrackIOErr(err) {
		w.tracking.done()
	}
	return n, err
}

func (w *wrappedConn) Close() error {
	w.tracking.done()
	w.closeOnce.Do(func() { w.closeErr = w.Conn.Close() })
	return w.closeErr
}

func (w *wrappedConn) Upstream() any { return w.Conn }

type wrappedPacketConn struct {
	network.PacketConn
	tracking  *connectionTracking
	closeOnce sync.Once
	closeErr  error
}

func (w *wrappedPacketConn) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	destination, err := w.PacketConn.ReadPacket(buffer)
	if shouldUntrackIOErr(err) {
		w.tracking.done()
	}
	return destination, err
}

func (w *wrappedPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	err := w.PacketConn.WritePacket(buffer, destination)
	if shouldUntrackIOErr(err) {
		w.tracking.done()
	}
	return err
}

func (w *wrappedPacketConn) Close() error {
	w.tracking.done()
	w.closeOnce.Do(func() { w.closeErr = w.PacketConn.Close() })
	return w.closeErr
}

func (w *wrappedPacketConn) Upstream() any { return w.PacketConn }
