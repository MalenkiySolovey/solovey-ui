//go:build linux

package privilegedbroker

import (
	"context"
	"errors"
)

// peerSupervisionAttestor is the owner-local HOW-HERE seam. The generic peer
// inspector supplies process and executable identity; the selected adapter
// binds that identity to one concrete supervisor proposition.
type peerSupervisionAttestor interface {
	Bind(context.Context, PeerIdentity, ClientManifest, string) (PeerIdentity, error)
}

func newPeerSupervisionAttestor(proof SupervisorProofKind) (peerSupervisionAttestor, error) {
	switch proof {
	case SupervisorProofSystemd:
		return systemdSupervisionAttestor{readCgroup: processSystemdCgroupUnit}, nil
	case SupervisorProofProcd:
		return procdSupervisionAttestor{inspector: NewProcdInspector(), observeCgroup: processProcdCgroup}, nil
	default:
		return nil, errors.New("broker supervisor proof adapter is unavailable")
	}
}
