package tracker

import (
	"context"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/trafficcontrol"
	"github.com/sagernet/sing-box/log"
)

func newTestConnTracker(t *testing.T) *ConnTracker {
	t.Helper()
	logger := log.NewNOPFactory().Logger()
	manager := outbound.NewManager(logger, outbound.NewRegistry(), endpoint.NewManager(logger, endpoint.NewRegistry()), "")
	manager.Initialize(func() (adapter.Outbound, error) { return fakeStatsOutbound{tag: "default"}, nil })
	if err := manager.Start(adapter.StartStateInitialize); err != nil {
		t.Fatal(err)
	}
	inventory := trafficcontrol.NewManager(manager)
	if err := inventory.Start(adapter.StartStateInitialize); err != nil {
		t.Fatal(err)
	}
	tracker := NewConnTracker(inventory)
	t.Cleanup(func() { tracker.Reset(); _ = inventory.Close(); _ = manager.Close() })
	return tracker
}

func TestRoutedAdmissionPrecedesActualInventory(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		for _, transport := range []string{"tcp", "packet"} {
			t.Run(transport+map[bool]string{true: "/accepted", false: "/rejected"}[allowed], func(t *testing.T) {
				connections := newTestConnTracker(t)
				observer := &testIPObserver{allow: allowed}
				stats := NewStatsTracker(observer)
				routed := NewRoutedTracker(stats, connections)
				metadata := adapter.InboundContext{Inbound: "in", User: "alice"}
				var closeFlow func() error
				if transport == "tcp" {
					conn := routed.RoutedConnection(context.Background(), newBlockingTestConn(), metadata, nil, nil)
					closeFlow = conn.Close
				} else {
					conn := routed.RoutedPacketConnection(context.Background(), terminalTestPacketConn{}, metadata, nil, nil)
					closeFlow = conn.Close
				}
				want := 0
				if allowed {
					want = 1
				}
				if count := connections.inventory.ConnectionsLen(); count != want {
					t.Fatalf("live inventory=%d, want %d", count, want)
				}
				if observer.calls != 1 {
					t.Fatalf("admission calls=%d", observer.calls)
				}
				if !allowed && (len(stats.GetStats()) != 0 || stats.inflight.Active() != 0) {
					t.Fatal("rejected traffic entered accounting")
				}
				_ = closeFlow()
				_ = closeFlow()
				if connections.inventory.ConnectionsLen() != 0 {
					t.Fatal("closed flow retained in inventory")
				}
			})
		}
	}
}
