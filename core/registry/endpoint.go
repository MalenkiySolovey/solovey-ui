package registry

import (
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/protocol/wireguard"
)

func EndpointRegistry() *endpoint.Registry {
	registry := endpoint.NewRegistry()

	wireguard.RegisterEndpoint(registry)
	registerTailscaleEndpoint(registry)

	return registry
}
