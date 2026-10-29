//go:build shtorm_extended && with_openconnect && with_gvisor

package registry

import (
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/protocol/openconnect"
)

const supportsOpenConnect = true

func registerOpenConnectEndpoint(r *endpoint.Registry) { openconnect.RegisterEndpoint(r) }
func registerOpenConnectDNS(r *dns.TransportRegistry)  { openconnect.RegisterDNSTransport(r) }
