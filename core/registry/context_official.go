//go:build !shtorm_extended

package registry

import (
	"context"

	sb "github.com/sagernet/sing-box"
)

func nativeContext(ctx context.Context) context.Context {
	return sb.Context(ctx, InboundRegistry(), OutboundRegistry(), EndpointRegistry(), DNSTransportRegistry(), ServiceRegistry(), CertificateProviderRegistry())
}
