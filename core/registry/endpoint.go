package registry

import (
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/protocol/wireguard"
)

func EndpointRegistry() *endpoint.Registry {
	registry := endpoint.NewRegistry()
	for _, entry := range endpointDeclarations() {
		entry.register(registry)
	}
	return registry
}

func endpointDeclarations() []declaration[*endpoint.Registry] {
	return []declaration[*endpoint.Registry]{
		{typeName: "wireguard", buildTag: "", compiled: true, register: wireguard.RegisterEndpoint},
		{typeName: "tailscale", buildTag: tailscaleBuildTag, compiled: supportsTailscale, register: registerTailscaleEndpoint},
	}
}
