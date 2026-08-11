package helper

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"time"
)

// ListenerExecutor has one bounded operation: connect to an already-fenced,
// exact socket. It cannot run commands, discover ports, or mutate a service.
type ListenerExecutor interface {
	Probe(context.Context, ListenerProbeRequest) (*ListenerProbeResult, error)
}

type systemListenerExecutor struct{}

func (systemListenerExecutor) Probe(ctx context.Context, request ListenerProbeRequest) (*ListenerProbeResult, error) {
	address, err := netip.ParseAddr(request.Address)
	if err != nil {
		return nil, err
	}
	dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(dialCtx, request.Network, net.JoinHostPort(address.String(), strconv.Itoa(request.Port)))
	if err != nil {
		// Connection refusal is a valid, redacted probe result rather than a
		// helper failure. Callers decide whether it blocks apply or rollback.
		return &ListenerProbeResult{Reachable: false, Detail: "listener_unreachable"}, nil
	}
	_ = connection.Close()
	owner, ownerErr := probeListenerOwner(ctx, address, request.Port, request.ExpectedPID)
	if ownerErr != nil {
		return &ListenerProbeResult{Reachable: true, Detail: "listener_owner_unknown"}, nil
	}
	return &ListenerProbeResult{Reachable: true, OwnerMatched: owner == request.ExpectedOwner, OwnerClass: owner, Detail: "listener_reachable"}, nil
}
