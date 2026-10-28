//go:build shtorm_extended

package registry

import (
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/bond"
	"github.com/sagernet/sing-box/protocol/failover"
)

const supportsNativeOutboundGroups = true

func registerBondOutbound(r *outbound.Registry)         { bond.RegisterOutbound(r) }
func registerCoreFailoverOutbound(r *outbound.Registry) { failover.RegisterOutbound(r) }
