//go:build shtorm_extended

package registry

import (
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/call"
)

const supportsCallOutbound = true

func registerCallOutbound(r *outbound.Registry) { call.RegisterOutbound(r) }
