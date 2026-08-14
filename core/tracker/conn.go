package tracker

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"
)

type ConnectionInfo struct {
	ID         string
	Conn       net.Conn
	PacketConn network.PacketConn
	Inbound    string
	Type       string // "tcp" or "udp"
	tracking   *connectionTracking
}

type ConnTracker struct {
	access      sync.Mutex
	connections map[string]*ConnectionInfo
	inflight    *trackerWaitGroup
	epoch       uint64
	nextID      atomic.Uint64
}

func NewConnTracker() *ConnTracker {
	return &ConnTracker{
		connections: make(map[string]*ConnectionInfo),
		inflight:    newTrackerWaitGroup(),
	}
}

func (c *ConnTracker) Reset() {
	c.access.Lock()
	connections := make([]*ConnectionInfo, 0, len(c.connections))
	for _, connInfo := range c.connections {
		connections = append(connections, connInfo)
	}
	c.connections = make(map[string]*ConnectionInfo)
	c.epoch++
	waitGroup := c.inflight
	c.inflight = newTrackerWaitGroup()
	c.access.Unlock()
	for _, connInfo := range connections {
		closeTrackedConnection(connInfo)
	}
	waitForTrackerIdle("connection tracker", waitGroup, trackerResetWaitTimeout)
}

func (c *ConnTracker) generateConnectionID() string {
	return "connection-" + strconv.FormatUint(c.nextID.Add(1), 10)
}

func (c *ConnTracker) RoutedConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) net.Conn {
	connID := c.generateConnectionID()
	connInfo := &ConnectionInfo{
		ID:      connID,
		Conn:    conn,
		Inbound: metadata.Inbound,
		Type:    "tcp",
	}

	tracking := c.trackConnection(connID, connInfo)
	wrapped := c.createWrappedConn(conn, tracking)
	c.replaceTrackedTCP(connID, connInfo, wrapped)
	return wrapped
}

func (c *ConnTracker) RoutedPacketConnection(ctx context.Context, conn network.PacketConn, metadata adapter.InboundContext, matchedRule adapter.Rule, matchOutbound adapter.Outbound) network.PacketConn {
	connID := c.generateConnectionID()
	connInfo := &ConnectionInfo{
		ID:         connID,
		PacketConn: conn,
		Inbound:    metadata.Inbound,
		Type:       "udp",
	}

	tracking := c.trackConnection(connID, connInfo)
	wrapped := c.createWrappedPacketConn(conn, tracking)
	c.replaceTrackedPacket(connID, connInfo, wrapped)
	return wrapped
}

func (c *ConnTracker) CloseConnByInbound(inbound string) int {
	c.access.Lock()
	connections := make([]*ConnectionInfo, 0)
	for connID, connInfo := range c.connections {
		if connInfo.Inbound == inbound {
			delete(c.connections, connID)
			connections = append(connections, connInfo)
		}
	}
	c.access.Unlock()
	for _, connInfo := range connections {
		closeTrackedConnection(connInfo)
	}
	return len(connections)
}

func (c *ConnTracker) trackConnection(connID string, connInfo *ConnectionInfo) *connectionTracking {
	c.access.Lock()
	defer c.access.Unlock()
	c.inflight.Add()
	tracking := &connectionTracking{tracker: c, connID: connID, epoch: c.epoch, waitGroup: c.inflight}
	connInfo.tracking = tracking
	c.connections[connID] = connInfo
	return tracking
}

func (c *ConnTracker) replaceTrackedTCP(connID string, expected *ConnectionInfo, wrapped net.Conn) {
	c.access.Lock()
	if current := c.connections[connID]; current == expected {
		current.Conn = wrapped
	}
	c.access.Unlock()
}

func (c *ConnTracker) replaceTrackedPacket(connID string, expected *ConnectionInfo, wrapped network.PacketConn) {
	c.access.Lock()
	if current := c.connections[connID]; current == expected {
		current.PacketConn = wrapped
	}
	c.access.Unlock()
}

func (c *ConnTracker) untrackConnection(connID string, epoch uint64) {
	c.access.Lock()
	defer c.access.Unlock()
	if epoch != c.epoch {
		return
	}
	delete(c.connections, connID)
}

// shouldUntrackIOErr reports whether err indicates the connection is done (peer closed, reset, etc.).
func shouldUntrackIOErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		// Temporary() is deprecated; a non-timeout net error indicates the connection is done.
		return !ne.Timeout()
	}
	return true
}

func (c *ConnTracker) createWrappedConn(conn net.Conn, tracking *connectionTracking) *wrappedConn {
	return &wrappedConn{
		Conn:     conn,
		tracking: tracking,
	}
}

func (c *ConnTracker) createWrappedPacketConn(conn network.PacketConn, tracking *connectionTracking) *wrappedPacketConn {
	return &wrappedPacketConn{
		PacketConn: conn,
		tracking:   tracking,
	}
}

type connectionTracking struct {
	tracker   *ConnTracker
	connID    string
	epoch     uint64
	waitGroup *trackerWaitGroup
	once      sync.Once
}

func (t *connectionTracking) done() {
	if t == nil {
		return
	}
	t.once.Do(func() {
		t.tracker.untrackConnection(t.connID, t.epoch)
		t.waitGroup.Done()
	})
}

func closeTrackedConnection(connInfo *ConnectionInfo) {
	if connInfo == nil {
		return
	}
	if connInfo.Conn != nil {
		_ = connInfo.Conn.Close()
	}
	if connInfo.PacketConn != nil {
		_ = connInfo.PacketConn.Close()
	}
	connInfo.tracking.done()
}

type wrappedConn struct {
	net.Conn
	tracking *connectionTracking
}

func (w *wrappedConn) doUntrack() {
	w.tracking.done()
}

func (w *wrappedConn) Read(b []byte) (int, error) {
	n, err := w.Conn.Read(b)
	if shouldUntrackIOErr(err) {
		w.doUntrack()
	}
	return n, err
}

func (w *wrappedConn) Write(b []byte) (int, error) {
	n, err := w.Conn.Write(b)
	if err != nil && shouldUntrackIOErr(err) {
		w.doUntrack()
	}
	return n, err
}

func (w *wrappedConn) Close() error {
	w.doUntrack()
	return w.Conn.Close()
}

func (w *wrappedConn) Upstream() any {
	return w.Conn
}

type wrappedPacketConn struct {
	network.PacketConn
	tracking *connectionTracking
}

func (w *wrappedPacketConn) doUntrack() {
	w.tracking.done()
}

func (w *wrappedPacketConn) ReadPacket(buffer *buf.Buffer) (destination M.Socksaddr, err error) {
	dest, err := w.PacketConn.ReadPacket(buffer)
	if shouldUntrackIOErr(err) {
		w.doUntrack()
	}
	return dest, err
}

func (w *wrappedPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	err := w.PacketConn.WritePacket(buffer, destination)
	if err != nil && shouldUntrackIOErr(err) {
		w.doUntrack()
	}
	return err
}

func (w *wrappedPacketConn) Close() error {
	w.doUntrack()
	return w.PacketConn.Close()
}

func (w *wrappedPacketConn) Upstream() any {
	return w.PacketConn
}
