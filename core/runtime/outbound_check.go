package runtime

import (
	"context"
	"errors"
	"net"
	"slices"
	"time"

	"github.com/sagernet/sing-box/adapter"
	urltest "github.com/sagernet/sing-box/common/urltest"
	"github.com/sagernet/sing/service"
)

const checkTimeout = 15 * time.Second

const (
	CheckOutboundErrorInvalidRequest  = "invalid_request"
	CheckOutboundErrorCoreUnavailable = "core_unavailable"
	CheckOutboundErrorNotFound        = "outbound_not_found"
	CheckOutboundErrorTimeout         = "outbound_check_timeout"
	CheckOutboundErrorCanceled        = "outbound_check_canceled"
	CheckOutboundErrorNetwork         = "outbound_check_network_failed"
	CheckOutboundErrorFailed          = "outbound_check_failed"
)

type CheckOutboundResult struct {
	OK    bool
	Delay uint16
	Error string
}

func (c *Core) CheckOutbound(ctx context.Context, tag string, link string) CheckOutboundResult {
	return c.CheckOutboundWithSource(ctx, tag, link, ProbeSourceManual)
}

func (c *Core) CheckOutboundWithSource(ctx context.Context, tag string, link string, source ProbeSource) (result CheckOutboundResult) {
	return c.checkOutbound(ctx, "", "", tag, link, source)
}

// CheckRuntimeOutbound binds a bounded manual probe to an accepted generation.
// The upstream URLTest RPC is fire-and-forget and does not honor caller
// cancellation; use the existing semantic probe owner instead.
func (c *Core) CheckRuntimeOutbound(ctx context.Context, generation, tag, link string) CheckOutboundResult {
	return c.checkRuntimeGroupOutbound(ctx, generation, "", tag, link)
}

// Group membership and the member's concrete dialer are resolved in the same
// mutation/lifecycle lease, before the bounded existing probe is admitted.
func (c *Core) CheckRuntimeGroupOutbound(ctx context.Context, generation, group, tag, link string) CheckOutboundResult {
	if group == "" {
		return CheckOutboundResult{Error: CheckOutboundErrorInvalidRequest}
	}
	return c.checkRuntimeGroupOutbound(ctx, generation, group, tag, link)
}

func (c *Core) checkRuntimeGroupOutbound(ctx context.Context, generation, group, tag, link string) CheckOutboundResult {
	if c == nil {
		return CheckOutboundResult{Error: CheckOutboundErrorCoreUnavailable}
	}
	if generation == "" {
		return CheckOutboundResult{Error: "stale_generation"}
	}
	select {
	case c.probeSlots <- struct{}{}:
		defer func() { <-c.probeSlots }()
	default:
		return CheckOutboundResult{Error: "runtime_limit_exceeded"}
	}
	return c.checkOutbound(ctx, generation, group, tag, link, ProbeSourceManual)
}

func (c *Core) checkOutbound(ctx context.Context, generation, group, tag, link string, source ProbeSource) (result CheckOutboundResult) {
	if c == nil {
		return CheckOutboundResult{Error: CheckOutboundErrorCoreUnavailable}
	}
	if source != ProbeSourceManual && source != ProbeSourceFailover && source != ProbeSourceDiagnostic {
		return CheckOutboundResult{Error: CheckOutboundErrorInvalidRequest}
	}
	var ticket probeTicket
	locked := false
	defer func() {
		if locked {
			c.mutation.Unlock()
		}
		if recovered := recover(); recovered != nil {
			result = CheckOutboundResult{Error: CheckOutboundErrorFailed}
		}
		if ticket.entry != nil {
			c.probeHealth.complete(ticket, result, time.Now())
		}
	}()
	// Keep the established mutation -> lifecycle lock order. Network execution
	// retains the lifecycle read lease, but does not hold the mutation mutex.
	c.mutation.Lock()
	locked = true
	err := c.withRuntime(func(current coreRuntime) error {
		if generation != "" && current.generation != generation {
			result.Error = "stale_generation"
			return nil
		}
		if group != "" {
			outbound, exists := current.outboundManager.Outbound(group)
			owner, isGroup := outbound.(adapter.OutboundGroup)
			if !exists || !isGroup || !slices.Contains(owner.All(), tag) {
				result.Error = "member_not_in_group"
				return nil
			}
		}
		// Resolve identity and reserve an observation atomically with respect to
		// target removal/replacement, then release the mutation lock for network.
		ob, ok := current.outboundManager.Outbound(tag)
		if !ok {
			result.Error = CheckOutboundErrorNotFound
			return nil
		}
		ticket = c.probeHealth.begin(tag, link, source, time.Now())
		locked = false
		c.mutation.Unlock()

		probeCtx, cancel := context.WithTimeout(ctx, checkTimeout)
		defer cancel()

		delay, probeErr := urltest.URLTest(probeCtx, link, ob)
		if probeErr != nil {
			result.Error = ClassifyOutboundCheckError(probeErr)
			return nil
		}
		result.OK = true
		result.Delay = delay
		if generation != "" {
			if history := service.PtrFromContext[urltest.HistoryStorage](current.ctx); history != nil {
				history.StoreURLTestHistory(tag, &adapter.URLTestHistory{Time: time.Now(), Delay: delay})
			}
		}
		return nil
	})
	if errors.Is(err, ErrCoreUnavailable) {
		result.Error = CheckOutboundErrorCoreUnavailable
	} else if err != nil {
		result.Error = CheckOutboundErrorFailed
	}
	return result
}

// ClassifyOutboundCheckError converts probe failures into a bounded, stable
// client-facing class without exposing network, TLS, filesystem, or panic text.
func ClassifyOutboundCheckError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return CheckOutboundErrorTimeout
	}
	if errors.Is(err, context.Canceled) {
		return CheckOutboundErrorCanceled
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		if networkError.Timeout() {
			return CheckOutboundErrorTimeout
		}
		return CheckOutboundErrorNetwork
	}
	return CheckOutboundErrorFailed
}
