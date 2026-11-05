//go:build shtorm_extended

package registry

import (
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/limiter/bandwidth"
	"github.com/sagernet/sing-box/protocol/limiter/connection"
	"github.com/sagernet/sing-box/protocol/limiter/rate"
	"github.com/sagernet/sing-box/protocol/limiter/traffic"
)

const supportsNativeLimiters = true

func registerBandwidthLimiter(r *outbound.Registry)  { bandwidth.RegisterOutbound(r) }
func registerConnectionLimiter(r *outbound.Registry) { connection.RegisterOutbound(r) }
func registerRateLimiter(r *outbound.Registry)       { rate.RegisterOutbound(r) }
func registerTrafficLimiter(r *outbound.Registry)    { traffic.RegisterOutbound(r) }
