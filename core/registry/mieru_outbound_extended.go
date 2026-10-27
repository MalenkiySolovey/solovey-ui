//go:build shtorm_extended

package registry

import (
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/mieru"
)

const supportsMieruOutbound = true

func registerMieruOutbound(r *outbound.Registry) { mieru.RegisterOutbound(r) }
