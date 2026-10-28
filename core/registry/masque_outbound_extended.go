//go:build shtorm_extended && with_quic

package registry

import (
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/masque"
)

const supportsMASQUEOutbound = true

func registerMASQUEOutbound(r *outbound.Registry) { masque.RegisterOutbound(r) }
