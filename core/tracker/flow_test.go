package tracker

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	tun "github.com/sagernet/sing-tun"
)

func TestActualFlowInventoryResetFencesPendingAndStaleCallbacks(t *testing.T) {
	connections := newTestConnTracker(t)
	stats := NewStatsTracker()
	routed := NewRoutedTracker(stats, connections)
	metadata := adapter.InboundContext{Inbound: "in", User: "user"}
	old := routed.RoutedFlow(context.Background(), metadata, nil, nil)
	pending := routed.RoutedFlow(context.Background(), metadata, nil, nil)
	handle := new(flowTestHandle)
	old.AttachFlow(handle)
	old.CountForward(7)
	old.CountReverse(3)
	if connections.inventory.ConnectionsLen() != 1 {
		t.Fatal("accepted flow not visible exactly once")
	}
	connections.Reset()
	if handle.closed.Load() != 1 || connections.inventory.ConnectionsLen() != 0 || stats.inflight.Active() != 0 {
		t.Fatal("reset did not close inventory and accounting once")
	}
	pendingHandle := new(flowTestHandle)
	pending.AttachFlow(pendingHandle)
	if pendingHandle.closed.Load() != 1 || connections.inventory.ConnectionsLen() != 0 || stats.inflight.Active() != 0 {
		t.Fatal("late attachment crossed reset generation")
	}
	stats.Reset()
	current := routed.RoutedFlow(context.Background(), metadata, nil, nil)
	current.AttachFlow(new(flowTestHandle))
	old.CloseFlow(tun.FlowCloseFinished)
	old.CountForward(100)
	current.CountForward(11)
	if count := connections.inventory.ConnectionsLen(); count != 1 {
		t.Fatalf("stale closure changed current inventory: %d", count)
	}
	var traffic int64
	for _, sample := range stats.GetStats() {
		if sample.Resource == "inbound" {
			traffic += sample.Traffic
		}
	}
	if traffic != 11 {
		t.Fatalf("flow accounting=%d, want11", traffic)
	}
	current.CloseFlow(tun.FlowCloseFinished)
	if connections.inventory.ConnectionsLen() != 0 {
		t.Fatal("completed flow retained")
	}
}

func TestRejectedL3FlowNeverProjectsOrCounts(t *testing.T) {
	connections := newTestConnTracker(t)
	observer := &testIPObserver{allow: false}
	stats := NewStatsTracker(observer)
	flow := NewRoutedTracker(stats, connections).RoutedFlow(context.Background(), adapter.InboundContext{Inbound: "in", User: "user"}, nil, nil)
	handle := new(flowTestHandle)
	flow.AttachFlow(handle)
	flow.CountForward(100)
	flow.CountReverse(100)
	if handle.closed.Load() != 1 || observer.calls != 1 || connections.inventory.ConnectionsLen() != 0 || len(stats.GetStats()) != 0 {
		t.Fatal("rejected L3 flow accepted")
	}
}

type flowTestHandle struct{ closed atomic.Int32 }

func (h *flowTestHandle) CloseFlow() { h.closed.Add(1) }
