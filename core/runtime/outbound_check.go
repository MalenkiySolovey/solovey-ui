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

func (c *Core) CheckOutbound(ctx context.Context, tag string, link string) (result CheckOutboundResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = CheckOutboundResult{Error: CheckOutboundErrorFailed}
		}
	}()
	err := c.withRuntime(func(current coreRuntime) error {
		ob, ok := current.outboundManager.Outbound(tag)
		if !ok {
			result.Error = CheckOutboundErrorNotFound
			return nil
		}

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
