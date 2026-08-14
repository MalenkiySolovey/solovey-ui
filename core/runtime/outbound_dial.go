package runtime

import (
	"context"
	"net"

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
	err = c.withRuntime(func(current coreRuntime) error {
		outbound, ok := current.outboundManager.Outbound(tag)
		if !ok {
			return ErrOutboundNotFound
		}
		var dialErr error
		connection, dialErr = outbound.DialContext(ctx, network, destination)
		return dialErr
	})
	return connection, err
}
