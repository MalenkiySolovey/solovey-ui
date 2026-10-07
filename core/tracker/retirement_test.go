package tracker

import (
	"context"
	"testing"

	"github.com/sagernet/sing-box/adapter"
)

func TestRetiredInventoryRejectsLateRouteCallbacks(t *testing.T) {
	connections := newTestConnTracker(t)
	stats := NewStatsTracker()
	routed := NewRoutedTracker(stats, connections)
	connections.Close()
	metadata := adapter.InboundContext{Inbound: "in"}
	conn := routed.RoutedConnection(context.Background(), newBlockingTestConn(), metadata, nil, nil)
	_ = conn.Close()
	packet := routed.RoutedPacketConnection(context.Background(), terminalTestPacketConn{}, metadata, nil, nil)
	_ = packet.Close()
	flow := routed.RoutedFlow(context.Background(), metadata, nil, nil)
	handle := new(flowTestHandle)
	flow.AttachFlow(handle)
	if handle.closed.Load() != 1 || connections.inventory.ConnectionsLen() != 0 || stats.inflight.Active() != 0 {
		t.Fatal("late callback reactivated retired generation")
	}
}
