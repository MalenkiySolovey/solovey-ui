package outbounds

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	C "github.com/sagernet/sing-box/constant"
)

func TestZapretOuterSocketProjection(t *testing.T) {
	domainTCP := func(kind string) zapretactivation.OuterSocket {
		return zapretactivation.OuterSocket{Kind: kind, Port: 443, Network: zapretactivation.OuterTCP}
	}
	for _, tc := range []struct {
		name, kind, fields string
		want               zapretactivation.OuterSocket
		deny               string
	}{
		{"http-ipv4", "http", `"server":"198.18.0.2","server_port":3128`, zapretactivation.OuterSocket{Kind: "http", Port: 3128, Family: zapretactivation.FamilyIPv4}, ""},
		{"http-hostname", "http", `"server":"proxy.example","server_port":443`, zapretactivation.OuterSocket{Kind: "http", Port: 443}, ""},
		{"http-detour", "http", `"server":"198.18.0.2","server_port":3128,"detour":"group"`, zapretactivation.OuterSocket{}, "owns egress selection"},
		{"http-detour-alias", "http", `"server":"198.18.0.2","server_port":3128,"Detour":"other"`, zapretactivation.OuterSocket{}, "ZAPRET_BINDING_INVALID"},
		{"http-no-port", "http", `"server":"198.18.0.2"`, zapretactivation.OuterSocket{}, "explicit outer port"},
		{"http-loopback", "http", `"server":"127.0.0.1","server_port":3128`, zapretactivation.OuterSocket{}, "admitted egress"},
		{"snell-v4", "snell", `"server":"proxy.example","server_port":443,"version":4`, zapretactivation.OuterSocket{Kind: "snell", Port: 443}, ""},
		{"snell-v6-logical-udp", "snell", `"server":"2001:db8::2","server_port":443,"version":6,"network":"udp"`, zapretactivation.OuterSocket{Kind: "snell", Port: 443, Family: zapretactivation.FamilyIPv6}, ""},
		{"snell-detoured", "snell", `"server":"proxy.example","server_port":443,"detour":"underlay"`, zapretactivation.OuterSocket{}, "owns egress selection"},
		{"group", "selector", `"outbounds":["member"]`, zapretactivation.OuterSocket{}, "no shared dial path"},
		{"hysteria-ipv6", "hysteria2", `"server":"2001:db8::2","server_port":443`, zapretactivation.OuterSocket{Kind: "hysteria2", Port: 443, Family: zapretactivation.FamilyIPv6}, ""},
		{"hysteria-realm", "hysteria2", `"realm":{}`, zapretactivation.OuterSocket{}, "dynamic peer sockets"},
		{"hysteria-hop", "hysteria2", `"server":"198.18.0.2","server_port":443,"server_ports":["443:444"]`, zapretactivation.OuterSocket{}, "every hop"},
		{"vless-logical-udp", "vless", `"server":"proxy.example","server_port":443,"network":"udp","multiplex":{"enabled":true}`, domainTCP("vless"), ""},
		{"vmess-ws", "vmess", `"server":"proxy.example","server_port":443,"transport":{"type":"ws","path":"/socket"}`, domainTCP("vmess"), ""},
		{"trojan-quic", "trojan", `"server":"proxy.example","server_port":443,"transport":{"type":"quic"}`, zapretactivation.OuterSocket{Kind: "trojan", Port: 443, Network: zapretactivation.OuterUDP}, ""},
		{"vless-xhttp-download", "vless", `"server":"proxy.example","server_port":443,"transport":{"type":"xhttp","download":{"detour":"elsewhere"}}`, zapretactivation.OuterSocket{}, "separate download or peer sockets"},
		{"tuic-ipv6", "tuic", `"server":"2001:db8::2","server_port":443,"network":"tcp"`, zapretactivation.OuterSocket{Kind: "tuic", Port: 443, Family: zapretactivation.FamilyIPv6}, ""},
		{"tuic-intermediate", "tuic", `"server":"proxy.example","server_port":443,"detour":"other"`, zapretactivation.OuterSocket{}, "owns egress selection"},
		{"shadowsocks-native", "shadowsocks", `"server":"proxy.example","server_port":443,"network":"tcp"`, zapretactivation.OuterSocket{Kind: "shadowsocks", Port: 443, Network: zapretactivation.OuterTCPUDP}, ""},
		{"shadowsocks-uot", "shadowsocks", `"server":"proxy.example","server_port":443,"udp_over_tcp":true`, domainTCP("shadowsocks"), ""},
		{"shadowsocks-multiplex", "shadowsocks", `"server":"proxy.example","server_port":443,"multiplex":{"enabled":true}`, domainTCP("shadowsocks"), ""},
		{"shadowsocks-plugin", "shadowsocks", `"server":"proxy.example","server_port":443,"plugin":"v2ray-plugin"`, zapretactivation.OuterSocket{}, "plugin owns the TCP connection"},
		{"socks4a", "socks", `"server":"proxy.example","server_port":443,"version":"4a"`, zapretactivation.OuterSocket{Kind: "socks", Port: 443}, ""},
		{"socks5-uot", "socks", `"server":"proxy.example","server_port":443,"udp_over_tcp":{"enabled":true}`, zapretactivation.OuterSocket{Kind: "socks", Port: 443}, ""},
		{"socks5-negotiated", "socks", `"server":"proxy.example","server_port":443,"network":"tcp"`, zapretactivation.OuterSocket{}, "server-selected relay"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !C.WithQUIC && (tc.kind == "hysteria2" || tc.name == "tuic-ipv6" || tc.name == "trojan-quic") {
				t.Skip("requires the QUIC Core build")
			}
			row := model.Outbound{Tag: "outer", Type: tc.kind, Options: json.RawMessage(`{` + tc.fields + `,"zapret":{"outboundRef":"outer","enabled":true,"strategyRef":"proof"}}`)}
			input, enabled, err := zapretRequest(row)
			if tc.deny != "" {
				if err == nil || !strings.Contains(err.Error(), tc.deny) || enabled {
					t.Fatalf("unqualified path crossed the admission boundary: %+v, %v", input, err)
				}
				return
			}
			if err != nil || !enabled || input.Outer != tc.want {
				t.Fatalf("unexpected outer socket: %+v, enabled=%t, %v", input, enabled, err)
			}
			projected, err := projectZapretRuntime(row, false, func(request zapretactivation.Request) (zapretactivation.Grant, error) {
				return zapretactivation.Grant{Request: request, StrategyRevision: strings.Repeat("a", 64), Generation: strings.Repeat("b", 64)}, nil
			})
			if err != nil || strings.Contains(string(projected.Options), `"zapret"`) || !strings.Contains(string(projected.Options), `"protect_path"`) {
				t.Fatalf("runtime projection did not replace the attachment with admission: %s, %v", projected.Options, err)
			}
			var before, after map[string]json.RawMessage
			if err := json.Unmarshal(row.Options, &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(projected.Options, &after); err != nil {
				t.Fatal(err)
			}
			for key, value := range before {
				if key != "zapret" && string(after[key]) != string(value) {
					t.Errorf("native option %q changed: %s -> %s", key, value, after[key])
				}
			}
		})
	}
}

func TestZapretProxyOffPreservesOrdinaryDetour(t *testing.T) {
	for _, kind := range []string{"http", "vless", "tuic", "socks", "shadowsocks", "snell"} {
		row := model.Outbound{Tag: "outer", Type: kind, Options: json.RawMessage(`{"server":"proxy.example","server_port":443,"detour":"group","zapret":{"outboundRef":"outer","enabled":false,"strategyRef":"proof"}}`)}
		projected, err := projectZapretOutbound(row)
		if err != nil || string(projected.Options) != `{"detour":"group","server":"proxy.example","server_port":443}` {
			t.Fatalf("OFF did not preserve the %s native detour: %s, %v", kind, projected.Options, err)
		}
	}
}
