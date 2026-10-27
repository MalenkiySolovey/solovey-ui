//go:build !shtorm_extended

package registry

import "github.com/sagernet/sing-box/adapter/outbound"

const supportsMieruOutbound = false

func registerMieruOutbound(*outbound.Registry) {}
