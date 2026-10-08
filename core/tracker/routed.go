package tracker

import (
	"context"
	"net"

	"github.com/MalenkiySolovey/solovey-ui/core/inboundidentity"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/network"
)

// RoutedTracker is the router's admission boundary. Policy runs before the
// official inventory can assign a live-flow identity or accept traffic.
type RoutedTracker struct {
	stats       *StatsTracker
	connections *ConnTracker
}

var _ adapter.ConnectionTracker = (*RoutedTracker)(nil)

func NewRoutedTracker(stats *StatsTracker, connections *ConnTracker) *RoutedTracker {
	return &RoutedTracker{stats: stats, connections: connections}
}

func (t *RoutedTracker) RoutedConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, rule adapter.Rule, outbound adapter.Outbound) net.Conn {
	if ctx.Err() != nil {
		_ = conn.Close()
		return conn
	}
	var result net.Conn = conn
	if !inboundidentity.Admission(ctx, func() {
		wrapped, admitted := t.stats.admitConnection(conn, metadata, outbound)
		result = wrapped
		if admitted {
			result = t.connections.RoutedConnection(ctx, wrapped, inboundidentity.InventoryMetadata(ctx, metadata), rule, outbound)
		}
	}) {
		_ = conn.Close()
	}
	return result
}

func (t *RoutedTracker) RoutedPacketConnection(ctx context.Context, conn network.PacketConn, metadata adapter.InboundContext, rule adapter.Rule, outbound adapter.Outbound) network.PacketConn {
	if ctx.Err() != nil {
		_ = conn.Close()
		return conn
	}
	var result network.PacketConn = conn
	if !inboundidentity.Admission(ctx, func() {
		wrapped, admitted := t.stats.admitPacketConnection(conn, metadata, outbound)
		result = wrapped
		if admitted {
			result = t.connections.RoutedPacketConnection(ctx, wrapped, inboundidentity.InventoryMetadata(ctx, metadata), rule, outbound)
		}
	}) {
		_ = conn.Close()
	}
	return result
}
