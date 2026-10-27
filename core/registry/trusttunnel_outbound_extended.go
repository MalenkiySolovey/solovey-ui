//go:build shtorm_extended && with_quic

package registry

import (
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/trusttunnel"
)

const supportsTrustTunnelOutbound = true

func registerTrustTunnelOutbound(r *outbound.Registry) { trusttunnel.RegisterOutbound(r) }
