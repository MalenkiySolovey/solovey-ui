//go:build !shtorm_extended

package registry

import "github.com/sagernet/sing-box/adapter/inbound"

const supportsMieruInbound = false

func registerMieruInbound(*inbound.Registry) {}
