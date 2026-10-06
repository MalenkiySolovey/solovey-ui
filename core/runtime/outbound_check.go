package runtime

import (
	"context"
	"errors"
	"net"
	"time"

	urltest "github.com/sagernet/sing-box/common/urltest"
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
