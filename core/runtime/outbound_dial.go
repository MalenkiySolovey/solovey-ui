package runtime

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
)

func (c *Core) ValidateOutbound(tag string) error {
	return c.withRuntime(func(current coreRuntime) error {
		if _, ok := current.outboundManager.Outbound(tag); !ok {
			return ErrOutboundNotFound
		}
		return nil
	})
}

func (c *Core) DialOutbound(ctx context.Context, tag, network string, destination M.Socksaddr) (connection net.Conn, err error) {
	var selected adapter.Outbound
	var generation context.Context
	err = c.withRuntime(func(current coreRuntime) error {
		outbound, ok := current.outboundManager.Outbound(tag)
		if !ok {
			return ErrOutboundNotFound
		}
		selected, generation = outbound, current.ctx
		c.outboundDials.Add(1)
		return nil
	})
	if err != nil {
		return nil, err
	}
	defer c.outboundDials.Done()

	// Network I/O must not hold the lease that Stop needs to retire the Box.
	// Keep caller cancellation and bind the admitted dial to its generation.
	dialContext, cancel := context.WithCancel(ctx)
	stopCancellation := context.AfterFunc(generation, cancel)
	defer func() { stopCancellation(); cancel() }()
	if generation.Err() != nil {
		return nil, ErrCoreUnavailable
	}
	connection, err = selected.DialContext(dialContext, network, destination)
	if generation.Err() != nil {
		if connection != nil {
			_ = connection.Close()
		}
		return nil, ErrCoreUnavailable
	}
	return connection, err
}
