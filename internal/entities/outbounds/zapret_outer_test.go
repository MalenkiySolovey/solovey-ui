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
		{"group", "selector", `"outbounds":["member"]`, zapretactivation.OuterSocket{}, "no shared dial path"},
		{"hysteria-ipv6", "hysteria2", `"server":"2001:db8::2","server_port":443`, zapretactivation.OuterSocket{Kind: "hysteria2", Port: 443, Family: zapretactivation.FamilyIPv6}, ""},
		{"hysteria-realm", "hysteria2", `"realm":{}`, zapretactivation.OuterSocket{}, "dynamic peer sockets"},
		{"hysteria-hop", "hysteria2", `"server":"198.18.0.2","server_port":443,"server_ports":["443:444"]`, zapretactivation.OuterSocket{}, "every hop"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.kind == "hysteria2" && !C.WithQUIC {
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
		})
	}
}

func TestZapretProxyOffPreservesOrdinaryDetour(t *testing.T) {
	row := model.Outbound{Tag: "outer", Type: "http", Options: json.RawMessage(`{"server":"proxy.example","server_port":443,"detour":"group","zapret":{"outboundRef":"outer","enabled":false,"strategyRef":"proof"}}`)}
	projected, err := projectZapretOutbound(row)
	if err != nil || string(projected.Options) != `{"detour":"group","server":"proxy.example","server_port":443}` {
		t.Fatalf("OFF did not preserve the native detour: %s, %v", projected.Options, err)
	}
}
