package registry

import (
	"context"
	"errors"

	"github.com/sagernet/sing-box/adapter"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

// These selected future contracts are schema witnesses only. Their product
// owners must implement eligibility before any constructor can activate them.
func preparedFeatureError() error { return errors.New("runtime feature is not enabled by the product") }

func registerMDNSSchema(registry *dns.TransportRegistry) {
	dns.RegisterTransport[option.MDNSDNSServerOptions](registry, "mdns", func(context.Context, log.ContextLogger, string, option.MDNSDNSServerOptions) (adapter.DNSTransport, error) {
		return nil, preparedFeatureError()
	})
}

func registerResolvedDNSSchema(registry *dns.TransportRegistry) {
	dns.RegisterTransport[option.ResolvedDNSServerOptions](registry, "resolved", func(context.Context, log.ContextLogger, string, option.ResolvedDNSServerOptions) (adapter.DNSTransport, error) {
		return nil, preparedFeatureError()
	})
}

func registerAPISchema(registry *boxservice.Registry) {
	boxservice.Register[option.APIServiceOptions](registry, "api", func(context.Context, log.ContextLogger, string, option.APIServiceOptions) (adapter.Service, error) {
		return nil, preparedFeatureError()
	})
}
