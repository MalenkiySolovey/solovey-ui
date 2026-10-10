package registry

import (
	"context"

	"github.com/MalenkiySolovey/solovey-ui/internal/entities/ssmcache"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/service/oomkiller"
	"github.com/sagernet/sing-box/service/resolved"
	"github.com/sagernet/sing-box/service/ssmapi"
)

func ServiceRegistry() *service.Registry {
	registry := service.NewRegistry()
	for _, entry := range serviceDeclarations() {
		if entry.register != nil {
			entry.register(registry)
		}
	}
	return registry
}

func serviceDeclarations() []declaration[*service.Registry] {
	return []declaration[*service.Registry]{
		{typeName: "resolved", platform: "linux", compiled: supportsResolved, runtimeDependency: DependencyResolve1, register: resolved.RegisterService},
		{typeName: "ssm-api", buildTag: "", compiled: true, register: registerSSMService},
		{typeName: "derp", buildTag: tailscaleBuildTag, compiled: supportsTailscale, register: registerDERPService},
		{typeName: "oom-killer", buildTag: "", compiled: true, register: oomkiller.RegisterService},
		{typeName: "api", productUnavailable: "PRODUCT_FEATURE_NOT_ENABLED", register: registerAPISchema},
		{typeName: "usbip-server", buildTag: "with_usbip", productUnavailable: "UNSUPPORTED_BY_PRODUCT"},
		{typeName: "usbip-client", buildTag: "with_usbip", productUnavailable: "UNSUPPORTED_BY_PRODUCT"},
		{typeName: "hysteria-realm", buildTag: "with_quic", productUnavailable: "UNSUPPORTED_BY_PRODUCT"},
	}
}

func registerSSMService(registry *service.Registry) {
	service.Register[option.SSMAPIServiceOptions](registry, "ssm-api", func(ctx context.Context, logger log.ContextLogger, tag string, options option.SSMAPIServiceOptions) (adapter.Service, error) {
		if options.CachePath != "" {
			store, err := ssmcache.New(options.CachePath)
			if err != nil {
				return nil, err
			}
			ctx = ssmapi.WithCacheStore(ctx, store)
		}
		return ssmapi.NewService(ctx, logger, tag, options)
	})
}
