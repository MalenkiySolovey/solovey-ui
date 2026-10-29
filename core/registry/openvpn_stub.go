//go:build !shtorm_extended || !with_openvpn || !with_gvisor

package registry

import (
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/dns"
)

const supportsOpenVPNClient = false

func registerOpenVPNClient(*endpoint.Registry)  {}
func registerOpenVPNDNS(*dns.TransportRegistry) {}
