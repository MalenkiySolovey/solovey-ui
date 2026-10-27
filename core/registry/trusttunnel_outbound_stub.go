//go:build !shtorm_extended || !with_quic

package registry

import "github.com/sagernet/sing-box/adapter/outbound"

const supportsTrustTunnelOutbound = false

func registerTrustTunnelOutbound(*outbound.Registry) {}
