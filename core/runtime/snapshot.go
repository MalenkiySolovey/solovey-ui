package runtime

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
)

type coreRuntime struct {
	ctx             context.Context
	inboundManager  adapter.InboundManager
	outboundManager adapter.OutboundManager
	serviceManager  adapter.ServiceManager
	endpointManager adapter.EndpointManager
	router          adapter.Router
	factory         log.Factory
}

func (c *Core) runtime() (coreRuntime, bool) {
	c.access.RLock()
	defer c.access.RUnlock()
	if !c.isRunning || c.instance == nil {
		return coreRuntime{}, false
	}
	return coreRuntime{
		ctx:             c.ctx,
		inboundManager:  c.inboundManager,
		outboundManager: c.outboundManager,
		serviceManager:  c.serviceManager,
		endpointManager: c.endpointManager,
		router:          c.router,
		factory:         c.factory,
	}, true
}

// withRuntime keeps the read lock for the complete operation. Use it when a
// resolved manager object must remain valid until a mutation/read finishes.
func (c *Core) withRuntime(fn func(coreRuntime) error) error {
	c.access.RLock()
	defer c.access.RUnlock()
	if !c.isRunning || c.instance == nil {
		return nil
	}
	return fn(coreRuntime{
		ctx:             c.ctx,
		inboundManager:  c.inboundManager,
		outboundManager: c.outboundManager,
		serviceManager:  c.serviceManager,
		endpointManager: c.endpointManager,
		router:          c.router,
		factory:         c.factory,
	})
}

func (c *Core) Router() adapter.Router {
	c.access.RLock()
	defer c.access.RUnlock()
	return c.router
}

func (c *Core) OutboundManager() adapter.OutboundManager {
	c.access.RLock()
	defer c.access.RUnlock()
	return c.outboundManager
}
