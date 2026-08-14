package runtime

import (
	"context"
	"errors"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
)

var (
	ErrCoreUnavailable  = errors.New("sing-box is not running")
	ErrAlreadyRunning   = errors.New("sing-box is already running")
	ErrOutboundNotFound = errors.New("outbound not found")
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

// withRuntime keeps the lifecycle read lock for the complete operation. The
// state lock is held only while resolving managers, so callbacks may safely
// update core-owned projections without racing Stop or deadlocking on access.
func (c *Core) withRuntime(fn func(coreRuntime) error) error {
	c.lifecycle.RLock()
	defer c.lifecycle.RUnlock()

	c.access.RLock()
	if !c.isRunning || c.instance == nil {
		c.access.RUnlock()
		return ErrCoreUnavailable
	}
	runtime := coreRuntime{
		ctx:             c.ctx,
		inboundManager:  c.inboundManager,
		outboundManager: c.outboundManager,
		serviceManager:  c.serviceManager,
		endpointManager: c.endpointManager,
		router:          c.router,
		factory:         c.factory,
	}
	c.access.RUnlock()
	return fn(runtime)
}

func (c *Core) withMutation(fn func(coreRuntime) error) error {
	c.mutation.Lock()
	defer c.mutation.Unlock()
	return c.withRuntime(fn)
}
