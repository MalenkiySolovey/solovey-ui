//go:build !shtorm_extended

package box

import (
	"context"
	"io"

	"github.com/sagernet/sing-box/adapter"
	boxCertificate "github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxService "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/route"
)

type nativeBoxState struct{}

func (*nativeBoxState) initialize(*Box)                      {}
func (*nativeBoxState) configure(*Box, option.Options) error { return nil }

func newEndpointManager(factory log.Factory, registry adapter.EndpointRegistry) *endpoint.Manager {
	return endpoint.NewManager(factory.NewLogger("endpoint"), registry)
}
func newInboundManager(factory log.Factory, registry adapter.InboundRegistry, endpoints adapter.EndpointManager) *inbound.Manager {
	return inbound.NewManager(factory.NewLogger("inbound"), registry, endpoints)
}
func newOutboundManager(factory log.Factory, registry adapter.OutboundRegistry, endpoints adapter.EndpointManager, final string) *outbound.Manager {
	return outbound.NewManager(factory.NewLogger("outbound"), registry, endpoints, final)
}
func newDNSTransportManager(factory log.Factory, registry adapter.DNSTransportRegistry, outbounds adapter.OutboundManager, final string) *dns.TransportManager {
	return dns.NewTransportManager(factory.NewLogger("dns/transport"), registry, outbounds, final)
}
func newServiceManager(factory log.Factory, registry adapter.ServiceRegistry) *boxService.Manager {
	return boxService.NewManager(factory.NewLogger("service"), registry)
}
func newCertificateManager(factory log.Factory, registry adapter.CertificateProviderRegistry) *boxCertificate.Manager {
	return boxCertificate.NewManager(factory.NewLogger("certificate-provider"), registry)
}
func newRouter(ctx context.Context, factory log.Factory, routes option.RouteOptions, dnsOptions option.DNSOptions) *route.Router {
	return route.NewRouter(ctx, factory, routes, dnsOptions)
}
func newLifecycleService(owner adapter.SimpleLifecycle, name string) adapter.LifecycleService {
	return adapter.NewLifecycleService(owner, name)
}
func (*nativeBoxState) start(s *Box, stage adapter.StartStage, owner adapter.Lifecycle) error {
	return adapter.Start(s.ctx, s.logger, stage, owner)
}
func (*nativeBoxState) startServices(s *Box, stage adapter.StartStage) error {
	return adapter.StartNamed(s.ctx, s.logger, stage, s.internalService)
}
func (*nativeBoxState) close(owner any) error { return owner.(io.Closer).Close() }
func (*nativeBoxState) finish() error         { return nil }
