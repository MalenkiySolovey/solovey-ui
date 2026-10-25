//go:build shtorm_extended

package registry

import (
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/protocol/mieru"
)

const supportsMieruInbound = true

func registerMieruInbound(r *inbound.Registry) { mieru.RegisterInbound(r) }
