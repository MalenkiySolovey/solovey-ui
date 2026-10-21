package tracker

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/MalenkiySolovey/solovey-ui/core/inboundidentity"

	"github.com/sagernet/sing-box/adapter"
	tun "github.com/sagernet/sing-tun"
)

func (c *ConnTracker) RoutedFlow(ctx context.Context, metadata adapter.InboundContext, rule adapter.Rule, outbound adapter.Outbound) tun.FlowTracker {
	c.access.Lock()
	defer c.access.Unlock()
	if c.closed {
		return rejectedFlow{}
	}
	return &inventoryFlow{owner: c, epoch: c.epoch, group: c.inflight, actual: c.inventory.RoutedFlow(ctx, metadata, rule, outbound)}
}

type inventoryFlow struct {
	access           sync.Mutex
	owner            *ConnTracker
	epoch            uint64
	group            *trackerWaitGroup
	actual           tun.FlowTracker
	handle           tun.FlowHandle
	attached, closed bool
}

func (f *inventoryFlow) AttachFlow(handle tun.FlowHandle) {
	f.owner.access.Lock()
	f.access.Lock()
	if f.closed || f.attached || f.owner.closed || f.epoch != f.owner.epoch {
		f.access.Unlock()
		f.owner.access.Unlock()
		handle.CloseFlow()
		return
	}
	f.attached, f.handle = true, handle
	f.group.Add()
	f.actual.AttachFlow(inventoryFlowHandle{f})
	f.access.Unlock()
	f.owner.access.Unlock()
}

type inventoryFlowHandle struct{ flow *inventoryFlow }

func (h inventoryFlowHandle) CloseFlow() {
	if handle := h.flow.finish(tun.FlowCloseReset); handle != nil {
		handle.CloseFlow()
	}
}

func (f *inventoryFlow) CountForward(n int) {
	f.access.Lock()
	defer f.access.Unlock()
	if f.attached && !f.closed {
		f.actual.CountForward(n)
	}
}
func (f *inventoryFlow) CountReverse(n int) {
	f.access.Lock()
	defer f.access.Unlock()
	if f.attached && !f.closed {
		f.actual.CountReverse(n)
	}
}
func (f *inventoryFlow) FlowEstablished() {
	f.access.Lock()
	defer f.access.Unlock()
	if f.attached && !f.closed {
		f.actual.FlowEstablished()
	}
}
func (f *inventoryFlow) finish(reason tun.FlowCloseReason) tun.FlowHandle {
	f.access.Lock()
	defer f.access.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	if f.attached {
		f.actual.CloseFlow(reason)
		f.group.Done()
	}
	return f.handle
}
func (f *inventoryFlow) CloseFlow(reason tun.FlowCloseReason) { f.finish(reason) }

func (c *StatsTracker) RoutedFlow(ctx context.Context, metadata adapter.InboundContext, rule adapter.Rule, outbound adapter.Outbound) tun.FlowTracker {
	var result tun.FlowTracker = rejectedFlow{}
	inboundidentity.Admission(ctx, func() { result, _ = c.admitFlow(ctx, metadata, outbound) })
	return result
}

func (c *StatsTracker) admitFlow(ctx context.Context, metadata adapter.InboundContext, outbound adapter.Outbound) (tun.FlowTracker, bool) {
	if c.observer != nil && !c.observer.ObserveAndAllow(metadata.User, sourceIPFromMetadata(metadata)) {
		return rejectedFlow{}, false
	}
	tag := ""
	if outbound != nil {
		tag = outbound.Tag()
	}
	c.access.Lock()
	read, write := c.getReadCountersLocked(metadata.Inbound, tag, metadata.User, inboundidentity.AccountingUserID(ctx, metadata))
	flow := &statsFlow{read: read, write: write, group: c.inflight}
	c.access.Unlock()
	return flow, true
}

type statsFlow struct {
	access           sync.Mutex
	read, write      []*atomic.Int64
	group            *trackerWaitGroup
	attached, closed bool
}

func (f *statsFlow) AttachFlow(handle tun.FlowHandle) {
	f.access.Lock()
	defer f.access.Unlock()
	if !f.closed && !f.attached {
		f.attached = true
		f.group.Add()
	}
}
func (f *statsFlow) CountForward(n int) {
	f.access.Lock()
	defer f.access.Unlock()
	if f.attached && !f.closed {
		for _, counter := range f.read {
			counter.Add(int64(n))
		}
	}
}
func (f *statsFlow) CountReverse(n int) {
	f.access.Lock()
	defer f.access.Unlock()
	if f.attached && !f.closed {
		for _, counter := range f.write {
			counter.Add(int64(n))
		}
	}
}
func (f *statsFlow) FlowEstablished() {}
func (f *statsFlow) CloseFlow(reason tun.FlowCloseReason) {
	f.access.Lock()
	defer f.access.Unlock()
	if !f.closed {
		f.closed = true
		if f.attached {
			f.group.Done()
		}
	}
}

func (t *RoutedTracker) RoutedFlow(ctx context.Context, metadata adapter.InboundContext, rule adapter.Rule, outbound adapter.Outbound) tun.FlowTracker {
	if ctx.Err() != nil {
		return rejectedFlow{}
	}
	var result tun.FlowTracker = rejectedFlow{}
	inboundidentity.Admission(ctx, func() {
		stats, admitted := t.stats.admitFlow(ctx, metadata, outbound)
		result = stats
		if admitted {
			result = &routedFlow{stats: stats, actual: t.connections.RoutedFlow(ctx, inboundidentity.InventoryMetadata(ctx, metadata), rule, outbound)}
		}
	})
	return result
}

type routedFlow struct{ stats, actual tun.FlowTracker }

func (f *routedFlow) AttachFlow(handle tun.FlowHandle) {
	f.stats.AttachFlow(handle)
	f.actual.AttachFlow(&routedFlowHandle{flow: f, handle: handle})
}
func (f *routedFlow) CountForward(n int) { f.stats.CountForward(n); f.actual.CountForward(n) }
func (f *routedFlow) CountReverse(n int) { f.stats.CountReverse(n); f.actual.CountReverse(n) }
func (f *routedFlow) FlowEstablished()   { f.stats.FlowEstablished(); f.actual.FlowEstablished() }
func (f *routedFlow) CloseFlow(reason tun.FlowCloseReason) {
	f.stats.CloseFlow(reason)
	f.actual.CloseFlow(reason)
}

type routedFlowHandle struct {
	flow   *routedFlow
	handle tun.FlowHandle
	closed atomic.Bool
}

func (h *routedFlowHandle) CloseFlow() {
	if h.closed.CompareAndSwap(false, true) {
		h.flow.CloseFlow(tun.FlowCloseReset)
		h.handle.CloseFlow()
	}
}

type rejectedFlow struct{}

func (rejectedFlow) AttachFlow(handle tun.FlowHandle) { handle.CloseFlow() }
func (rejectedFlow) CountForward(int)                 {}
func (rejectedFlow) CountReverse(int)                 {}
func (rejectedFlow) FlowEstablished()                 {}
func (rejectedFlow) CloseFlow(tun.FlowCloseReason)    {}
