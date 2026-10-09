package registry

import (
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport"
	"github.com/sagernet/sing-box/dns/transport/dhcp"
	"github.com/sagernet/sing-box/dns/transport/fakeip"
	"github.com/sagernet/sing-box/dns/transport/hosts"
	"github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/dns/transport/mdns"
	"github.com/sagernet/sing-box/dns/transport/quic"
	"github.com/sagernet/sing-box/service/resolved"
)

func DNSTransportRegistry() *dns.TransportRegistry {
	registry := dns.NewTransportRegistry()
	for _, entry := range dnsDeclarations() {
		if entry.register != nil {
			entry.register(registry)
		}
	}
	return registry
}

func dnsDeclarations() []declaration[*dns.TransportRegistry] {
	return []declaration[*dns.TransportRegistry]{
		{typeName: "tcp", buildTag: "", compiled: true, register: transport.RegisterTCP},
		{typeName: "udp", buildTag: "", compiled: true, register: transport.RegisterUDP},
		{typeName: "tls", buildTag: "", compiled: true, register: transport.RegisterTLS},
		{typeName: "https", buildTag: "", compiled: true, register: transport.RegisterHTTPS},
		{typeName: "hosts", buildTag: "", compiled: true, register: hosts.RegisterTransport},
		{typeName: "local", buildTag: "", compiled: true, register: local.RegisterTransport},
		{typeName: "fakeip", buildTag: "", compiled: true, register: fakeip.RegisterTransport},
		{typeName: "quic", buildTag: "with_quic", compiled: C.WithQUIC, register: quic.RegisterTransport},
		{typeName: "h3", buildTag: "with_quic", compiled: C.WithQUIC, register: quic.RegisterHTTP3Transport},
		{typeName: "dhcp", buildTag: "", compiled: true, register: dhcp.RegisterTransport},
		{typeName: "tailscale", buildTag: tailscaleBuildTag, compiled: supportsTailscale, register: registerTailscaleTransport},
		{typeName: "mdns", compiled: true, runtimeDependency: DependencyMulticast, register: mdns.RegisterTransport},
		{typeName: "resolved", platform: "linux", compiled: supportsResolved, runtimeDependency: DependencyResolve1, register: resolved.RegisterTransport},
	}
}
