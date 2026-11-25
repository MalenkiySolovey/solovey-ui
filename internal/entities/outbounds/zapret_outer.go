package outbounds

import (
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

// Identify the pinned provider's fixed network endpoint. The logical network
// requested through a proxy may differ from the socket opened by this dialer.
func zapretOuterSocket(row model.Outbound) (zapretactivation.OuterSocket, error) {
	var socket zapretactivation.OuterSocket
	fail := func(reason string) (zapretactivation.OuterSocket, error) {
		return socket, fmt.Errorf("%w: %s", ErrZapretPacketPathUnqualified, reason)
	}
	var server option.ServerOptions
	var network zapretactivation.OuterNetwork
	switch row.Type {
	case C.TypeDirect:
		return socket, nil
	case C.TypeHTTP, C.TypeAnyTLS, C.TypeShadowTLS, C.TypeSSH, C.TypeSnell, C.TypeTUIC:
		if row.Type == C.TypeTUIC && !C.WithQUIC {
			return fail("this Core build does not support TUIC")
		}
		if err := json.Unmarshal(row.Options, &server); err != nil {
			return fail("invalid fixed proxy endpoint")
		}
	case C.TypeVLESS, C.TypeVMess, C.TypeTrojan:
		var native struct {
			option.ServerOptions
			Transport *struct{ Type string } `json:"transport"`
		}
		if err := json.Unmarshal(row.Options, &native); err != nil {
			return fail("invalid proxy transport")
		}
		server, network = native.ServerOptions, zapretactivation.OuterTCP
		if native.Transport != nil {
			switch native.Transport.Type {
			case C.V2RayTransportTypeHTTP, C.V2RayTransportTypeWebsocket, C.V2RayTransportTypeGRPC, C.V2RayTransportTypeHTTPUpgrade:
			case C.V2RayTransportTypeQUIC:
				if !C.WithQUIC {
					return fail("this Core build does not support the QUIC transport")
				}
				network = zapretactivation.OuterUDP
			default:
				return fail(fmt.Sprintf("transport %q has no qualified fixed outer path; check for separate download or peer sockets", native.Transport.Type))
			}
		}
	case C.TypeSOCKS:
		var native option.SOCKSOutboundOptions
		if err := json.Unmarshal(row.Options, &native); err != nil {
			return fail("invalid SOCKS proxy endpoint")
		}
		switch native.Version {
		case "4", "4a":
		case "", "5":
			// The native DetourDialer does not enforce the logical NetworkList.
			if native.UDPOverTCP == nil || !native.UDPOverTCP.Enabled {
				return fail("SOCKS5 UDP ASSOCIATE can use a server-selected relay address and port; the fixed path requires UDP over TCP")
			}
		default:
			return fail("invalid SOCKS version")
		}
		server = native.ServerOptions
	case C.TypeShadowsocks:
		var native option.ShadowsocksOutboundOptions
		if err := json.Unmarshal(row.Options, &native); err != nil {
			return fail("invalid Shadowsocks proxy endpoint")
		}
		if native.Plugin != "" {
			return fail("the Shadowsocks plugin owns the TCP connection and has no qualified socket handoff")
		}
		server, network = native.ServerOptions, zapretactivation.OuterTCPUDP
		// Both provider mux clients open a TCP session even for logical UDP.
		if (native.UDPOverTCP != nil && native.UDPOverTCP.Enabled) || (native.Multiplex != nil && native.Multiplex.Enabled) {
			network = zapretactivation.OuterTCP
		}
	case C.TypeHysteria2:
		if !C.WithQUIC {
			return fail("this Core build does not support Hysteria2")
		}
		var native option.Hysteria2OutboundOptions
		if err := json.Unmarshal(row.Options, &native); err != nil {
			return fail("invalid Hysteria2 proxy endpoint")
		}
		if native.Realm != nil {
			return fail("Hysteria2 Realm uses rendezvous and dynamic peer sockets outside fixed-endpoint admission")
		}
		if len(native.ServerPorts) != 0 {
			return fail("Hysteria2 port hopping needs admission for every hop; select a fixed server port")
		}
		server = native.ServerOptions
	default:
		return fail("the outbound has no qualified shared dialer")
	}
	address := server.Build()
	if !address.IsValid() || server.ServerPort == 0 {
		return fail("set a valid proxy server and an explicit outer port")
	}
	family := zapretactivation.FamilyFromProfile
	if ip := address.Addr.Unmap(); ip.IsValid() {
		if ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsLoopback() {
			return fail("the proxy endpoint must be reachable over the admitted egress, not a local or scoped address")
		}
		if ip.Is4() {
			family = zapretactivation.FamilyIPv4
		} else {
			family = zapretactivation.FamilyIPv6
		}
	}
	return zapretactivation.OuterSocket{Kind: row.Type, Port: server.ServerPort, Family: family, Network: network}, nil
}
