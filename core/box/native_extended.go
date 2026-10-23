//go:build shtorm_extended

package box

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/sagernet/sing-box/adapter"
	boxCertificate "github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/adapter/provider"
	boxService "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/route"
	"github.com/sagernet/sing/service"
)

// Only the native ABI differs. The Box, runtime generation, trackers and product
// owners stay shared. Each generation gets one private Shtorm lifecycle scope.
type nativeBoxState struct {
	scope     *adapter.Scope
	providers *provider.Manager
	started   map[adapter.Lifecycle]bool
}

func (n *nativeBoxState) initialize(s *Box) {
	n.scope = adapter.NewScope(s.ctx, s.logger)
	n.started = make(map[adapter.Lifecycle]bool)
	registry := service.FromContext[adapter.ProviderRegistry](s.ctx)
	if registry != nil {
		n.providers = provider.NewManager(registry)
		service.MustRegister[adapter.ProviderManager](s.ctx, n.providers)
	}
}

func (n *nativeBoxState) configure(s *Box, options option.Options) error {
	if n.providers == nil {
		return errors.New("missing native provider registry")
	}
	// Remote subscription state stays with its existing Component/EndpointIR
	// owner. Raw native provider definitions cannot bypass that ownership seam.
	if len(options.Providers) != 0 {
		return errors.New("native provider configuration requires the subscription owner")
	}
	s.internalService = append(s.internalService, route.NewReferenceManager(s.ctx, s.logFactory.NewLogger("reference"), options))
	return nil
}

func newEndpointManager(_ log.Factory, registry adapter.EndpointRegistry) *endpoint.Manager {
	return endpoint.NewManager(registry)
}
func newInboundManager(_ log.Factory, registry adapter.InboundRegistry, endpoints adapter.EndpointManager) *inbound.Manager {
	return inbound.NewManager(registry, endpoints)
}
func newOutboundManager(_ log.Factory, registry adapter.OutboundRegistry, endpoints adapter.EndpointManager, final string) *outbound.Manager {
	return outbound.NewManager(registry, endpoints, final)
}
func newDNSTransportManager(_ log.Factory, registry adapter.DNSTransportRegistry, outbounds adapter.OutboundManager, final string) *dns.TransportManager {
	return dns.NewTransportManager(registry, outbounds, final)
}
func newServiceManager(_ log.Factory, registry adapter.ServiceRegistry) *boxService.Manager {
	return boxService.NewManager(registry)
}
func newCertificateManager(_ log.Factory, registry adapter.CertificateProviderRegistry) *boxCertificate.Manager {
	return boxCertificate.NewManager(registry)
}
func newRouter(ctx context.Context, factory log.Factory, routes option.RouteOptions, dnsOptions option.DNSOptions) *route.Router {
	return route.NewRouter(ctx, factory, "router", routes, dnsOptions)
}

type simpleLifecycleService struct {
	adapter.SimpleLifecycle
	name string
}

func (s *simpleLifecycleService) Name() string { return s.name }
func (s *simpleLifecycleService) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	scope.Add(s.SimpleLifecycle.Close)
	return s.SimpleLifecycle.Start()
}
func newLifecycleService(owner adapter.SimpleLifecycle, name string) adapter.LifecycleService {
	return &simpleLifecycleService{SimpleLifecycle: owner, name: name}
}
func (n *nativeBoxState) start(s *Box, stage adapter.StartStage, owner adapter.Lifecycle) error {
	n.started[owner] = true
	if err := n.scope.Start(fmt.Sprintf("%T", owner), owner, stage); err != nil {
		return err
	}
	if owner == s.outbound && n.providers != nil {
		n.started[n.providers] = true
		return n.scope.Start("provider", n.providers, stage)
	}
	return nil
}
func (n *nativeBoxState) startServices(s *Box, stage adapter.StartStage) error {
	for _, owner := range s.internalService {
		n.started[owner] = true
		if err := n.scope.Start(owner.Name(), owner, stage); err != nil {
			return err
		}
	}
	return nil
}
func (n *nativeBoxState) close(owner any) error {
	if lifecycle, ok := owner.(adapter.Lifecycle); ok && n.started[lifecycle] {
		delete(n.started, lifecycle)
		return n.scope.Remove(lifecycle)
	}
	if closer, ok := owner.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
func (n *nativeBoxState) finish() error {
	if n.scope != nil {
		return n.scope.Close()
	}
	return nil
}
