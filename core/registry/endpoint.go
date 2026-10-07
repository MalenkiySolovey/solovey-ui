package registry

import (
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/protocol/wireguard"
)

func EndpointRegistry() *endpoint.Registry {
	registry := endpoint.NewRegistry()
	for _, entry := range endpointDeclarations() {
		if entry.register != nil {
			entry.register(registry)
		}
	}
	return registry
}

func endpointDeclarations() []declaration[*endpoint.Registry] {
	return []declaration[*endpoint.Registry]{
		{typeName: "wireguard", buildTag: "", compiled: true, register: wireguard.RegisterEndpoint},
		{typeName: "tailscale", buildTag: tailscaleBuildTag, compiled: supportsTailscale, register: registerTailscaleEndpoint},
		{typeName: "openconnect", buildTag: "with_openconnect", productUnavailable: "UNSUPPORTED_BY_PRODUCT"},
		{typeName: "openvpn-client", buildTag: "with_openvpn", productUnavailable: "UNSUPPORTED_BY_PRODUCT"},
		{typeName: "openvpn-server", buildTag: "with_openvpn", productUnavailable: "UNSUPPORTED_BY_PRODUCT"},
	}
}
