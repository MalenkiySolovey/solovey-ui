//go:build shtorm_extended && with_openvpn && with_gvisor

package registry

import (
	"github.com/sagernet/sing-box/adapter/endpoint"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/openvpn"
)

const supportsOpenVPNClient = true

func registerOpenVPNClient(r *endpoint.Registry) {
	endpoint.Register[option.OpenVPNClientEndpointOptions](r, C.TypeOpenVPNClient, openvpn.NewClientEndpoint)
}
func registerOpenVPNDNS(r *dns.TransportRegistry) { openvpn.RegisterDNSTransport(r) }
