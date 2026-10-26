//go:build shtorm_extended

package registry

import (
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/protocol/mieru"
	"github.com/sagernet/sing-box/protocol/mtproxy"
	"github.com/sagernet/sing-box/protocol/ssh"
)

const supportsMieruInbound = true
const supportsSSHInbound = true
const supportsMTProxyInbound = true

func registerMieruInbound(r *inbound.Registry)   { mieru.RegisterInbound(r) }
func registerSSHInbound(r *inbound.Registry)     { ssh.RegisterInbound(r) }
func registerMTProxyInbound(r *inbound.Registry) { mtproxy.RegisterInbound(r) }
