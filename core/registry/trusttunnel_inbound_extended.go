//go:build shtorm_extended && with_quic

package registry

import (
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/protocol/trusttunnel"
)

const supportsTrustTunnelInbound = true

func registerTrustTunnelInbound(r *inbound.Registry) { trusttunnel.RegisterInbound(r) }
