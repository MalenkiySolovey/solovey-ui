package registry

import (
	"context"

	sb "github.com/sagernet/sing-box"
	"github.com/sagernet/sing/service"
)

// Context preserves cancellation and explicit context values, but starts a new
// service registry and option registries. No mutable manager from a live box is
// inherited by a validation or replacement generation.
func Context(parent context.Context) context.Context {
	ctx := service.ContextWithRegistry(parent, service.NewRegistry())
	return sb.Context(ctx, InboundRegistry(), OutboundRegistry(), EndpointRegistry(), DNSTransportRegistry(), ServiceRegistry(), CertificateProviderRegistry())
}
