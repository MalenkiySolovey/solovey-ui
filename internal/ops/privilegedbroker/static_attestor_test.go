package privilegedbroker

import (
	"context"
	"fmt"
	"net"
)

type StaticAttestor struct{ Peer PeerIdentity }

func (a StaticAttestor) Attest(context.Context, *net.UnixConn, Role) (PeerIdentity, error) {
	if a.Peer.Revision == "" {
		return PeerIdentity{}, fmt.Errorf("static peer is absent")
	}
	return a.Peer, nil
}

func (a StaticAttestor) Recheck(context.Context, PeerIdentity, Role) error { return nil }
