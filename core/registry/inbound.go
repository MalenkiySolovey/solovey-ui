package registry

import (
	"github.com/sagernet/sing-box/adapter/inbound"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/protocol/anytls"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/protocol/http"
	"github.com/sagernet/sing-box/protocol/hysteria"
	"github.com/sagernet/sing-box/protocol/hysteria2"
	"github.com/sagernet/sing-box/protocol/mixed"
	"github.com/sagernet/sing-box/protocol/naive"
	"github.com/sagernet/sing-box/protocol/redirect"
	"github.com/sagernet/sing-box/protocol/shadowsocks"
	"github.com/sagernet/sing-box/protocol/shadowtls"
	"github.com/sagernet/sing-box/protocol/socks"
	"github.com/sagernet/sing-box/protocol/trojan"
	"github.com/sagernet/sing-box/protocol/tuic"
	"github.com/sagernet/sing-box/protocol/tun"
	"github.com/sagernet/sing-box/protocol/vless"
	"github.com/sagernet/sing-box/protocol/vmess"
)

func InboundRegistry() *inbound.Registry {
	registry := inbound.NewRegistry()
	for _, entry := range inboundDeclarations() {
		if entry.register != nil {
			entry.register(registry)
		}
	}
	return registry
}

func inboundDeclarations() []declaration[*inbound.Registry] {
	return []declaration[*inbound.Registry]{
		{typeName: "tun", buildTag: "", compiled: true, register: tun.RegisterInbound},
		{typeName: "redirect", buildTag: "", compiled: true, register: redirect.RegisterRedirect},
		{typeName: "tproxy", buildTag: "", compiled: true, register: redirect.RegisterTProxy},
		{typeName: "direct", buildTag: "", compiled: true, register: direct.RegisterInbound},
		{typeName: "socks", buildTag: "", compiled: true, register: socks.RegisterInbound},
		{typeName: "http", buildTag: "", compiled: true, register: http.RegisterInbound},
		{typeName: "mixed", buildTag: "", compiled: true, register: mixed.RegisterInbound},
		{typeName: "shadowsocks", buildTag: "", compiled: true, register: shadowsocks.RegisterInbound},
		{typeName: "vmess", buildTag: "", compiled: true, register: vmess.RegisterInbound},
		{typeName: "trojan", buildTag: "", compiled: true, register: trojan.RegisterInbound},
		{typeName: "naive", buildTag: "", compiled: true, register: naive.RegisterInbound},
		{typeName: "shadowtls", buildTag: "", compiled: true, register: shadowtls.RegisterInbound},
		{typeName: "vless", buildTag: "", compiled: true, register: vless.RegisterInbound},
		{typeName: "anytls", buildTag: "", compiled: true, register: anytls.RegisterInbound},
		{typeName: C.TypeSnell, productUnavailable: "PRODUCT_FEATURE_NOT_ENABLED", register: registerSnellInboundSchema},
		{typeName: C.TypeCloudflared, buildTag: "with_cloudflared", productUnavailable: "UNSUPPORTED_BY_PRODUCT"},
		{typeName: "hysteria", buildTag: "with_quic", compiled: C.WithQUIC, register: hysteria.RegisterInbound},
		{typeName: "tuic", buildTag: "with_quic", compiled: C.WithQUIC, register: tuic.RegisterInbound},
		{typeName: "hysteria2", buildTag: "with_quic", compiled: C.WithQUIC, register: hysteria2.RegisterInbound},
	}
}
