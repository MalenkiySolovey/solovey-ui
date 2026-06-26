package runtime

import (
	"context"
	"sync"

	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/core/tracker"

	sb "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
)

type Core struct {
	access          sync.RWMutex
	ctx             context.Context
	isRunning       bool
	instance        *corebox.Box
	inboundManager  adapter.InboundManager
	outboundManager adapter.OutboundManager
	serviceManager  adapter.ServiceManager
	endpointManager adapter.EndpointManager
	router          adapter.Router
	factory         log.Factory
	ipObserver      tracker.IPObserver
}

func NewCore(observers ...tracker.IPObserver) *Core {
	ctx := context.Background()
	ctx = sb.Context(
		ctx,
		registry.InboundRegistry(),
		registry.OutboundRegistry(),
		registry.EndpointRegistry(),
		registry.DNSTransportRegistry(),
		registry.ServiceRegistry(),
	)
	core := &Core{
		ctx:       ctx,
		isRunning: false,
		instance:  nil,
	}
	if len(observers) > 0 {
		core.ipObserver = observers[0]
	}
	return core
}
