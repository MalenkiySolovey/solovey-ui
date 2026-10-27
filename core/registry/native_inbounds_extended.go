//go:build shtorm_extended

package registry

import (
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/protocol/bond"
	"github.com/sagernet/sing-box/protocol/call"
	"github.com/sagernet/sing-box/protocol/failover"
	"github.com/sagernet/sing-box/protocol/mieru"
	"github.com/sagernet/sing-box/protocol/mtproxy"
	"github.com/sagernet/sing-box/protocol/ssh"
	"github.com/sagernet/sing-box/protocol/sudoku"
)

const supportsMieruInbound = true
const supportsSSHInbound = true
const supportsMTProxyInbound = true
const supportsSudokuInbound = true

const supportsBondInbound = true
const supportsCoreFailoverInbound = true
const supportsCallInbound = true

func registerMieruInbound(r *inbound.Registry)   { mieru.RegisterInbound(r) }
func registerSSHInbound(r *inbound.Registry)     { ssh.RegisterInbound(r) }
func registerMTProxyInbound(r *inbound.Registry) { mtproxy.RegisterInbound(r) }
func registerSudokuInbound(r *inbound.Registry)  { sudoku.RegisterInbound(r) }

func registerBondInbound(r *inbound.Registry)         { bond.RegisterInbound(r) }
func registerCoreFailoverInbound(r *inbound.Registry) { failover.RegisterInbound(r) }
func registerCallInbound(r *inbound.Registry)         { call.RegisterInbound(r) }
