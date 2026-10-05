package registry

import (
	"github.com/sagernet/sing-box/adapter/outbound"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/protocol/anytls"
	"github.com/sagernet/sing-box/protocol/block"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/protocol/group"
	"github.com/sagernet/sing-box/protocol/http"
	"github.com/sagernet/sing-box/protocol/hysteria"
	"github.com/sagernet/sing-box/protocol/hysteria2"
	_ "github.com/sagernet/sing-box/protocol/naive/quic"
	"github.com/sagernet/sing-box/protocol/shadowsocks"
	"github.com/sagernet/sing-box/protocol/shadowtls"
	"github.com/sagernet/sing-box/protocol/socks"
	"github.com/sagernet/sing-box/protocol/ssh"
	"github.com/sagernet/sing-box/protocol/tor"
	"github.com/sagernet/sing-box/protocol/trojan"
	"github.com/sagernet/sing-box/protocol/tuic"
	"github.com/sagernet/sing-box/protocol/vless"
	"github.com/sagernet/sing-box/protocol/vmess"
)

func OutboundRegistry() *outbound.Registry {
	registry := outbound.NewRegistry()
	for _, entry := range outboundDeclarations() {
		entry.register(registry)
	}
	return registry
}

func outboundDeclarations() []declaration[*outbound.Registry] {
	return []declaration[*outbound.Registry]{
		{typeName: "direct", buildTag: "", compiled: true, register: direct.RegisterOutbound},
		{typeName: "block", buildTag: "", compiled: true, register: block.RegisterOutbound},
		{typeName: "selector", buildTag: "", compiled: true, register: group.RegisterSelector},
		{typeName: "urltest", buildTag: "", compiled: true, register: group.RegisterURLTest},
		{typeName: "socks", buildTag: "", compiled: true, register: socks.RegisterOutbound},
		{typeName: "http", buildTag: "", compiled: true, register: http.RegisterOutbound},
		{typeName: "shadowsocks", buildTag: "", compiled: true, register: shadowsocks.RegisterOutbound},
		{typeName: "vmess", buildTag: "", compiled: true, register: vmess.RegisterOutbound},
		{typeName: "trojan", buildTag: "", compiled: true, register: trojan.RegisterOutbound},
		{typeName: "naive", buildTag: "with_naive_outbound", compiled: SupportsNaiveOutbound, register: registerNaiveOutbound},
		{typeName: "tor", buildTag: "", compiled: true, register: tor.RegisterOutbound},
		{typeName: "ssh", buildTag: "", compiled: true, register: ssh.RegisterOutbound},
		{typeName: "shadowtls", buildTag: "", compiled: true, register: shadowtls.RegisterOutbound},
		{typeName: "vless", buildTag: "", compiled: true, register: vless.RegisterOutbound},
		{typeName: "anytls", buildTag: "", compiled: true, register: anytls.RegisterOutbound},
		{typeName: "hysteria", buildTag: "with_quic", compiled: C.WithQUIC, register: hysteria.RegisterOutbound},
		{typeName: "tuic", buildTag: "with_quic", compiled: C.WithQUIC, register: tuic.RegisterOutbound},
		{typeName: "hysteria2", buildTag: "with_quic", compiled: C.WithQUIC, register: hysteria2.RegisterOutbound},
	}
}
