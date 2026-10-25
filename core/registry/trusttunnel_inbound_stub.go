//go:build !shtorm_extended || !with_quic

package registry

import "github.com/sagernet/sing-box/adapter/inbound"

const supportsTrustTunnelInbound = false

func registerTrustTunnelInbound(*inbound.Registry) {}
