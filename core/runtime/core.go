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
	"github.com/sagernet/sing/service"
)

type Core struct {
	lifecycle         sync.RWMutex
	mutation          sync.Mutex
	access            sync.RWMutex
	ctx               context.Context
	isRunning         bool
	instance          *corebox.Box
	inboundManager    adapter.InboundManager
	outboundManager   adapter.OutboundManager
	serviceManager    adapter.ServiceManager
	endpointManager   adapter.EndpointManager
	router            adapter.Router
	factory           log.Factory
	ipObserver        tracker.IPObserver
	statsTracker      *tracker.StatsTracker
	connTracker       *tracker.ConnTracker
	managerGeneration uint64
	effectiveInbounds map[string]InboundRuntimeRecord
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
		ctx:               ctx,
		isRunning:         false,
		instance:          nil,
		effectiveInbounds: make(map[string]InboundRuntimeRecord),
	}
	if len(observers) > 0 {
		core.ipObserver = observers[0]
	}
	core.ctx = service.ContextWith(core.ctx, core)
	return core
}
