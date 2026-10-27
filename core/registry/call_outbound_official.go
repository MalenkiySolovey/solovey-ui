//go:build !shtorm_extended

package registry

import "github.com/sagernet/sing-box/adapter/outbound"

const supportsCallOutbound = false

func registerCallOutbound(*outbound.Registry) {}
