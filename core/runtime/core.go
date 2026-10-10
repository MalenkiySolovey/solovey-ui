package runtime

import (
	"context"
	"sync"
	"sync/atomic"

	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/core/tracker"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
)

type Core struct {
	lifecycle         sync.RWMutex
	mutation          sync.RWMutex
	access            sync.RWMutex
	ctx               context.Context
	parentContext     context.Context
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
	probeHealth       recentProbeHealth
	privateAPI        *privateAPI
	prepareAPI        func(context.Context, *option.Options) (context.Context, *privateAPI, error)
	lifecycleState    string
	logSubscribers    atomic.Int32
	snapshotReaders   atomic.Int32
	probeSlots        chan struct{}
}

func NewCore(observers ...tracker.IPObserver) *Core {
	ctx := context.Background()
	ctx = registry.Context(ctx)
	core := &Core{
		ctx:               ctx,
		isRunning:         false,
		instance:          nil,
		lifecycleState:    "stopped",
		probeSlots:        make(chan struct{}, 4),
		effectiveInbounds: make(map[string]InboundRuntimeRecord),
	}
	if len(observers) > 0 {
		core.ipObserver = observers[0]
	}
	core.ctx = service.ContextWith(core.ctx, core)
	return core
}
