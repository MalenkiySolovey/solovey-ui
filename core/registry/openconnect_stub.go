//go:build !shtorm_extended || !with_openconnect || !with_gvisor

package registry

import (
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/dns"
)

const supportsOpenConnect = false

func registerOpenConnectEndpoint(*endpoint.Registry) {}
func registerOpenConnectDNS(*dns.TransportRegistry)  {}
