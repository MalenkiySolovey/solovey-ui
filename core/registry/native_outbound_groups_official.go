//go:build !shtorm_extended

package registry

import "github.com/sagernet/sing-box/adapter/outbound"

const supportsNativeOutboundGroups = false

func registerBondOutbound(*outbound.Registry)         {}
func registerCoreFailoverOutbound(*outbound.Registry) {}
