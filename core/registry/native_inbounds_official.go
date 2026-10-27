//go:build !shtorm_extended

package registry

import "github.com/sagernet/sing-box/adapter/inbound"

const supportsMieruInbound = false
const supportsSSHInbound = false
const supportsMTProxyInbound = false
const supportsSudokuInbound = false

const supportsBondInbound = false
const supportsCoreFailoverInbound = false
const supportsCallInbound = false

func registerMieruInbound(*inbound.Registry)   {}
func registerSSHInbound(*inbound.Registry)     {}
func registerMTProxyInbound(*inbound.Registry) {}
func registerSudokuInbound(*inbound.Registry)  {}

func registerBondInbound(*inbound.Registry)         {}
func registerCoreFailoverInbound(*inbound.Registry) {}
func registerCallInbound(*inbound.Registry)         {}
