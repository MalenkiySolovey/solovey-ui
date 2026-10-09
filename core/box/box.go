package box

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/inboundidentity"
	corelogging "github.com/MalenkiySolovey/solovey-ui/core/logging"
	"github.com/MalenkiySolovey/solovey-ui/core/tracker"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/sagernet/sing-box/adapter"
	boxCertificate "github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxService "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/common/certificate"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/httpclient"
	"github.com/sagernet/sing-box/common/netns"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/common/trafficcontrol"
	"github.com/sagernet/sing-box/common/urltest"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/experimental"
	"github.com/sagernet/sing-box/experimental/cachefile"
	"github.com/sagernet/sing-box/experimental/clashmode"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/route"
	sbCommon "github.com/sagernet/sing/common"
	F "github.com/sagernet/sing/common/format"
	"github.com/sagernet/sing/common/ntp"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/pause"
)

var _ adapter.SimpleLifecycle = (*Box)(nil)

type Box struct {
	ctx                 context.Context
	cancel              context.CancelFunc
	createdAt           time.Time
	logFactory          log.Factory
	logger              log.ContextLogger
	network             *route.NetworkManager
	endpoint            *endpoint.Manager
	inbound             *inbound.Manager
	outbound            *outbound.Manager
	service             *boxService.Manager
	certificateProvider *boxCertificate.Manager
	httpClient          *httpclient.Manager
	traffic             *trafficcontrol.Manager
	dnsTransport        *dns.TransportManager
	dnsRouter           *dns.Router
	connection          *route.ConnectionManager
	router              *route.Router
	internalService     []adapter.LifecycleService
	statsTracker        *tracker.StatsTracker
	connTracker         *tracker.ConnTracker
	inboundIdentity     *inboundidentity.Owner
	done                chan struct{}
	closeOnce           sync.Once
	closeErr            error
	initialized         map[adapter.Lifecycle]bool
}

func NewBox(options Options) (_ *Box, err error) {
	createdAt := time.Now()
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = service.ContextWithDefaultRegistry(ctx)

	endpointRegistry := service.FromContext[adapter.EndpointRegistry](ctx)
	inboundRegistry := service.FromContext[adapter.InboundRegistry](ctx)
	outboundRegistry := service.FromContext[adapter.OutboundRegistry](ctx)
	dnsTransportRegistry := service.FromContext[adapter.DNSTransportRegistry](ctx)
	serviceRegistry := service.FromContext[adapter.ServiceRegistry](ctx)
	certificateRegistry := service.FromContext[adapter.CertificateProviderRegistry](ctx)

	if endpointRegistry == nil {
		return nil, common.NewError("missing endpoint registry in context")
	}
	if inboundRegistry == nil {
		return nil, common.NewError("missing inbound registry in context")
	}
	if outboundRegistry == nil {
		return nil, common.NewError("missing outbound registry in context")
	}
	if dnsTransportRegistry == nil {
		return nil, common.NewError("missing DNS transport registry in context")
	}
	if serviceRegistry == nil {
		return nil, common.NewError("missing service registry in context")
	}
	if certificateRegistry == nil {
		return nil, common.NewError("missing certificate provider registry in context")
	}
	// Namespace privileges and durable configuration are outside the current
	// product contract. The required empty manager still participates in context.
	if len(options.NetworkNamespaces) != 0 {
		return nil, common.NewError("network namespaces are unavailable in this product")
	}
	if err = validateConfiguredTags(options.Options); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)

	ctx = pause.WithDefaultManager(ctx)
	experimentalOptions := sbCommon.PtrValueOrDefault(options.Experimental)
	var needCacheFile bool
	var needClashAPI bool
	var needV2RayAPI bool
	if experimentalOptions.CacheFile != nil && experimentalOptions.CacheFile.Enabled {
		needCacheFile = true
	}
	if experimentalOptions.ClashAPI != nil {
		needClashAPI = true
	}
	if experimentalOptions.V2RayAPI != nil && experimentalOptions.V2RayAPI.Listen != "" {
		needV2RayAPI = true
	}
	platformInterface := service.FromContext[adapter.PlatformInterface](ctx)
	var defaultLogWriter io.Writer
	if platformInterface != nil {
		defaultLogWriter = io.Discard
	}
	var logFactory log.Factory
	logFactory, err = corelogging.NewFactory(log.Options{
		Context:       ctx,
		Options:       sbCommon.PtrValueOrDefault(options.Log),
		DefaultWriter: defaultLogWriter,
		BaseTime:      createdAt,
	})
	if err != nil {
		cancel()
		return nil, common.NewError("create log factory", err)
	}
	s := &Box{ctx: ctx, cancel: cancel, createdAt: createdAt, logFactory: logFactory,
		logger: logFactory.Logger(), done: make(chan struct{}),
		initialized: make(map[adapter.Lifecycle]bool), inboundIdentity: inboundidentity.NewOwner()}
	defer func() {
		if err != nil {
			err = errors.Join(err, s.Close())
		}
	}()
	service.MustRegister[log.Factory](ctx, logFactory)
	service.MustRegisterPtr(ctx, urltest.NewHistoryStorage())

	certificateOptions := sbCommon.PtrValueOrDefault(options.Certificate)
	if C.IsAndroid || certificateOptions.Store != "" && certificateOptions.Store != C.CertificateStoreSystem ||
		len(certificateOptions.Certificate) > 0 ||
		len(certificateOptions.CertificatePath) > 0 ||
		len(certificateOptions.CertificateDirectoryPath) > 0 {
		certificateStore, err := certificate.NewStore(ctx, logFactory.NewLogger("certificate"), certificateOptions)
		if err != nil {
			return nil, err
		}
		service.MustRegister[adapter.CertificateStore](ctx, certificateStore)
		s.internalService = append(s.internalService, certificateStore)
	}
	namespaceManager, err := netns.NewManager(logFactory.NewLogger("netns"), nil, nil)
	if err != nil {
		return nil, common.NewError("initialize namespace manager", err)
	}
	service.MustRegister[adapter.NetworkNamespaceManager](ctx, namespaceManager)
	s.internalService = append(s.internalService, namespaceManager)

	routeOptions := sbCommon.PtrValueOrDefault(options.Route)
	dnsOptions := sbCommon.PtrValueOrDefault(options.DNS)
	endpointManager := endpoint.NewManager(logFactory.NewLogger("endpoint"), endpointRegistry)
	inboundManager := inbound.NewManager(logFactory.NewLogger("inbound"), constructionRegistry{inboundRegistry}, endpointManager)
	outboundManager := outbound.NewManager(logFactory.NewLogger("outbound"), outboundRegistry, endpointManager, routeOptions.Final)
	dnsTransportManager := dns.NewTransportManager(logFactory.NewLogger("dns/transport"), dnsTransportRegistry, outboundManager, dnsOptions.Final)
	serviceManager := boxService.NewManager(logFactory.NewLogger("service"), serviceRegistry)
	certificateManager := boxCertificate.NewManager(logFactory.NewLogger("certificate-provider"), certificateRegistry)
	s.endpoint, s.inbound, s.outbound = endpointManager, inboundManager, outboundManager
	s.dnsTransport, s.service, s.certificateProvider = dnsTransportManager, serviceManager, certificateManager

	service.MustRegister[adapter.EndpointManager](ctx, endpointManager)
	service.MustRegister[adapter.InboundManager](ctx, inboundManager)
	service.MustRegister[adapter.OutboundManager](ctx, outboundManager)
	service.MustRegister[adapter.DNSTransportManager](ctx, dnsTransportManager)
	service.MustRegister[adapter.ServiceManager](ctx, serviceManager)
	service.MustRegister[adapter.CertificateProviderManager](ctx, certificateManager)

	dnsRouter, err := dns.NewRouter(ctx, logFactory, dnsOptions)
	if err != nil {
		return nil, common.NewError("initialize DNS router", err)
	}
	s.dnsRouter = dnsRouter
	service.MustRegister[adapter.DNSRouter](ctx, dnsRouter)
	service.MustRegister[adapter.DNSRuleSetUpdateValidator](ctx, dnsRouter)
	connectionManager := route.NewConnectionManager(logFactory.NewLogger("connection"))
	s.connection = connectionManager
	service.MustRegister[adapter.ConnectionManager](ctx, connectionManager)

	networkManager, err := route.NewNetworkManager(ctx, logFactory.NewLogger("network"), routeOptions, dnsOptions)
	if err != nil {
		return nil, common.NewError("initialize network manager", err)
	}
	service.MustRegister[adapter.NetworkManager](ctx, networkManager)
	s.network = networkManager
	s.httpClient = httpclient.NewManager(ctx, logFactory.NewLogger("httpclient"), options.HTTPClients, routeOptions.DefaultHTTPClient)
	service.MustRegister[adapter.HTTPClientManager](ctx, s.httpClient)
	router := route.NewRouter(ctx, logFactory, routeOptions, dnsOptions)
	s.router = router
	service.MustRegister[adapter.Router](ctx, router)
	err = router.Initialize(routeOptions.Rules, routeOptions.RuleSet)
	if err != nil {
		return nil, common.NewError("initialize router", err)
	}
	s.traffic = trafficcontrol.NewManager(outboundManager)
	service.MustRegisterPtr(ctx, s.traffic)
	s.internalService = append(s.internalService, s.traffic)
	if needClashAPI {
		clashOptions := sbCommon.PtrValueOrDefault(experimentalOptions.ClashAPI)
		mode := clashmode.NewManager(ctx, logFactory.NewLogger("clash-mode"), clashOptions.DefaultMode, clashmode.CalculateModeList(options.Options))
		service.MustRegisterPtr(ctx, mode)
		s.internalService = append(s.internalService, mode)
	}
	ntpOptions := sbCommon.PtrValueOrDefault(options.NTP)
	var timeService *tls.TimeServiceWrapper
	if ntpOptions.Enabled {
		timeService = new(tls.TimeServiceWrapper)
		service.MustRegister[ntp.TimeService](ctx, timeService)
	}
	for i, transportOptions := range dnsOptions.Servers {
		tag := indexedOptionTag(i, transportOptions.Tag)
		err = dnsTransportManager.Create(
			ctx,
			logFactory.NewLogger(F.ToString("dns/", transportOptions.Type, "[", tag, "]")),
			tag,
			transportOptions.Type,
			transportOptions.Options,
		)
		if err != nil {
			return nil, common.NewError("initialize DNS server[", i, "]", err)
		}
	}
	err = dnsRouter.Initialize(dnsOptions.Rules)
	if err != nil {
		return nil, common.NewError("initialize dns router", err)
	}
	for i, endpointOptions := range options.Endpoints {
		tag := indexedOptionTag(i, endpointOptions.Tag)
		endpointCtx := adapter.WithContext(ctx, &adapter.InboundContext{Outbound: tag})
		err = endpointManager.Create(
			endpointCtx,
			router,
			logFactory.NewLogger(F.ToString("endpoint/", endpointOptions.Type, "[", tag, "]")),
			tag,
			endpointOptions.Type,
			endpointOptions.Options,
		)
		if err != nil {
			return nil, common.NewError("initialize endpoint["+F.ToString(i)+"] "+tag, err)
		}
	}
	for i, inboundOptions := range options.Inbounds {
		tag := indexedOptionTag(i, inboundOptions.Tag)
		boundRouter, epoch, bindErr := s.inboundIdentity.Prepare(router, tag, options.IdentityBindings)
		if bindErr != nil {
			return nil, bindErr
		}
		err = inboundManager.Create(
			ctx,
			boundRouter,
			logFactory.NewLogger(F.ToString("inbound/", inboundOptions.Type, "[", tag, "]")),
			tag,
			inboundOptions.Type,
			inboundOptions.Options,
		)
		if err != nil {
			return nil, common.NewError("initialize inbound[", i, "] ", tag, err)
		}
		s.inboundIdentity.Publish(tag, epoch)
	}
	for i, outboundOptions := range options.Outbounds {
		tag := indexedOptionTag(i, outboundOptions.Tag)
		outboundCtx := ctx
		if tag != "" {
			// Some outbound constructors read the current outbound tag from context.
			outboundCtx = adapter.WithContext(outboundCtx, &adapter.InboundContext{
				Outbound: tag,
			})
		}
		err = outboundManager.Create(
			outboundCtx,
			router,
			logFactory.NewLogger(F.ToString("outbound/", outboundOptions.Type, "[", tag, "]")),
			tag,
			outboundOptions.Type,
			outboundOptions.Options,
		)
		if err != nil {
			return nil, common.NewError("initialize outbound["+F.ToString(i)+"] "+tag, err)
		}
	}
	for i, serviceOptions := range options.Services {
		tag := indexedOptionTag(i, serviceOptions.Tag)
		err = serviceManager.Create(
			ctx,
			logFactory.NewLogger(F.ToString("service/", serviceOptions.Type, "[", tag, "]")),
			tag,
			serviceOptions.Type,
			serviceOptions.Options,
		)
		if err != nil {
			return nil, common.NewError("initialize service["+F.ToString(i)+"]"+tag, err)
		}
	}
	for i, providerOptions := range options.CertificateProviders {
		tag := indexedOptionTag(i, providerOptions.Tag)
		err = certificateManager.Create(ctx, logFactory.NewLogger(F.ToString("certificate-provider/", providerOptions.Type, "[", tag, "]")), tag, providerOptions.Type, providerOptions.Options)
		if err != nil {
			return nil, common.NewError("initialize certificate provider[", i, "]", err)
		}
	}
	outboundManager.Initialize(func() (adapter.Outbound, error) {
		return direct.NewOutbound(
			ctx,
			router,
			logFactory.NewLogger("outbound/direct"),
			"direct",
			option.DirectOutboundOptions{},
		)
	})
	dnsTransportManager.Initialize(func() (adapter.DNSTransport, error) {
		return local.NewTransport(
			ctx,
			logFactory.NewLogger("dns/local"),
			"local",
			option.LocalDNSServerOptions{},
		)
	})
	s.httpClient.Initialize(func() (*httpclient.ManagedTransport, error) {
		return httpclient.NewTransport(ctx, logFactory.NewLogger("httpclient"), "", option.HTTPClientOptions{DefaultOutbound: true})
	})
	if platformInterface != nil {
		err = platformInterface.Initialize(networkManager)
		if err != nil {
			return nil, common.NewError("initialize platform interface", err)
		}
	}
	statsTracker := tracker.NewStatsTracker(options.IPObserver)
	connTracker := tracker.NewConnTracker(s.traffic)
	s.statsTracker, s.connTracker = statsTracker, connTracker
	router.AppendTracker(tracker.NewRoutedTracker(statsTracker, connTracker))

	if needCacheFile {
		cacheFile := cachefile.New(ctx, logFactory.NewLogger("cache-file"), sbCommon.PtrValueOrDefault(experimentalOptions.CacheFile))
		service.MustRegister[adapter.CacheFile](ctx, cacheFile)
		s.internalService = append(s.internalService, cacheFile)
	}
	if needClashAPI {
		clashAPIOptions := sbCommon.PtrValueOrDefault(experimentalOptions.ClashAPI)
		clashServer, err := experimental.NewClashServer(ctx, logFactory.(log.ObservableFactory), clashAPIOptions)
		if err != nil {
			return nil, common.NewError(err, "create clash-server")
		}
		s.internalService = append(s.internalService, clashServer)
	}
	if needV2RayAPI {
		v2rayServer, err := experimental.NewV2RayServer(logFactory.NewLogger("v2ray-api"), sbCommon.PtrValueOrDefault(experimentalOptions.V2RayAPI))
		if err != nil {
			return nil, common.NewError(err, "create v2ray-server")
		}
		if v2rayServer.StatsService() != nil {
			router.AppendTracker(v2rayServer.StatsService())
			s.internalService = append(s.internalService, v2rayServer)
			service.MustRegister[adapter.V2RayServer](ctx, v2rayServer)
		}
	}
	if ntpOptions.Enabled {
		if ntpOptions.WriteToSystem {
			if err = adapter.CheckSecurityFeature(ctx, "NTP `write_to_system`"); err != nil {
				return nil, err
			}
		}
		ntpDialer, err := dialer.New(ctx, ntpOptions.DialerOptions, ntpOptions.ServerIsDomain())
		if err != nil {
			return nil, common.NewError(err, "create NTP service")
		}
		ntpService := ntp.NewService(ntp.Options{
			Context:       ctx,
			Dialer:        ntpDialer,
			Logger:        logFactory.NewLogger("ntp"),
			Server:        ntpOptions.ServerOptions.Build(),
			Interval:      time.Duration(ntpOptions.Interval),
			WriteToSystem: ntpOptions.WriteToSystem,
		})
		timeService.TimeService = ntpService
		s.internalService = append(s.internalService, adapter.NewLifecycleService(ntpService, "ntp service"))
	}
	return s, nil
}
