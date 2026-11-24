package outbounds

import (
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

// The pinned HTTP and conventional Hysteria2 paths use the outbound's
// shared dialer. Only assert the outer transport's protocol, port and family.
func zapretOuterSocket(row model.Outbound) (zapretactivation.OuterSocket, error) {
	var socket zapretactivation.OuterSocket
	fail := func(reason string) (zapretactivation.OuterSocket, error) {
		return socket, fmt.Errorf("%w: %s", ErrZapretPacketPathUnqualified, reason)
	}
	var server option.ServerOptions
	switch row.Type {
	case C.TypeDirect:
		return socket, nil
	case C.TypeHTTP:
		if err := json.Unmarshal(row.Options, &server); err != nil {
			return fail("invalid HTTP proxy endpoint")
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
	return zapretactivation.OuterSocket{Kind: row.Type, Port: server.ServerPort, Family: family}, nil
}
