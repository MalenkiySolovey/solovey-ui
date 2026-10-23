//go:build shtorm_extended

package registry

import (
	"context"

	sb "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/provider"
)

func nativeContext(ctx context.Context) context.Context {
	// Native provider types stay unregistered until their existing product owner
	// supplies a capability-gated integration; an empty registry is not support.
	return sb.Context(ctx, InboundRegistry(), OutboundRegistry(), EndpointRegistry(), provider.NewRegistry(), DNSTransportRegistry(), ServiceRegistry(), CertificateProviderRegistry())
}
