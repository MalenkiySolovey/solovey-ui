package tracker

import (
	"context"
	"net"

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
	wrapped, admitted := t.stats.admitConnection(conn, metadata, outbound)
	if !admitted {
		return wrapped
	}
	return t.connections.RoutedConnection(ctx, wrapped, metadata, rule, outbound)
}

func (t *RoutedTracker) RoutedPacketConnection(ctx context.Context, conn network.PacketConn, metadata adapter.InboundContext, rule adapter.Rule, outbound adapter.Outbound) network.PacketConn {
	if ctx.Err() != nil {
		_ = conn.Close()
		return conn
	}
	wrapped, admitted := t.stats.admitPacketConnection(conn, metadata, outbound)
	if !admitted {
		return wrapped
	}
	return t.connections.RoutedPacketConnection(ctx, wrapped, metadata, rule, outbound)
}
