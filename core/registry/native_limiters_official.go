//go:build !shtorm_extended

package registry

import "github.com/sagernet/sing-box/adapter/outbound"

const supportsNativeLimiters = false

func registerBandwidthLimiter(*outbound.Registry)  {}
func registerConnectionLimiter(*outbound.Registry) {}
func registerRateLimiter(*outbound.Registry)       {}
func registerTrafficLimiter(*outbound.Registry)    {}
