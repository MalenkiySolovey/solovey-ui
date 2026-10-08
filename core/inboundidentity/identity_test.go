package inboundidentity

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	N "github.com/sagernet/sing/common/network"
)

func acceptedEpoch(t *testing.T, owner *Owner, tag string, id uint) *Epoch {
	t.Helper()
	_, epoch, err := owner.Prepare(nil, tag, []Binding{{Inbound: tag, Principal: "alice", ClientID: id}})
	if err != nil {
		t.Fatal(err)
	}
	owner.Publish(tag, epoch)
	return epoch
}

func TestIdentityIsAuthenticatedInboundBinding(t *testing.T) {
	owner := NewOwner()
	first := acceptedEpoch(t, owner, "in-a", 7)
	second := acceptedEpoch(t, owner, "in-b", 9)
	for _, item := range []struct {
		epoch         *Epoch
		inbound, user string
		want          uint
	}{
		{first, "in-a", "alice", 7}, {second, "in-b", "alice", 9},
		{first, "in-b", "alice", 0}, {first, "in-a", "unknown", 0},
	} {
		metadata := adapter.InboundContext{Inbound: item.inbound, User: item.user}
		ctx, err := item.epoch.bind(t.Context(), metadata)
		if err != nil {
			t.Fatal(err)
		}
		actual := InventoryMetadata(ctx, metadata)
		binding, known := owner.Resolve(actual.Inbound, actual.User)
		if known != (item.want != 0) || known && binding.ClientID != item.want {
			t.Fatal("wrong authenticated client association")
		}
		if metadata.User != item.user {
			t.Fatal("counter/admission user changed")
		}
	}
	if _, known := owner.Resolve("in-a", "alice"); known {
		t.Fatal("display name was treated as identity")
	}
}

func TestReplacementRejectsOldParentsAndReusedPrincipal(t *testing.T) {
	owner := NewOwner()
	epoch := acceptedEpoch(t, owner, "in", 7)
	metadata := adapter.InboundContext{Inbound: "in", User: "alice"}
	ctx, err := epoch.bind(t.Context(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	oldToken := InventoryMetadata(ctx, metadata).User
	_ = acceptedEpoch(t, owner, "in", 42)
	if _, known := owner.Resolve("in", oldToken); known {
		t.Fatal("old identity mapped to replacement client")
	}
	if _, err := epoch.bind(t.Context(), metadata); err == nil {
		t.Fatal("old authenticated parent admitted a new stream")
	}
	if Admission(ctx, func() { t.Fatal("late stream was accepted") }) {
		t.Fatal("old admission lease accepted")
	}
	owner.Revoke("in")
	owner.Close()
}

func TestRevokeWaitsForAcceptedInventoryPublication(t *testing.T) {
	owner := NewOwner()
	epoch := acceptedEpoch(t, owner, "in", 7)
	ctx, err := epoch.bind(t.Context(), adapter.InboundContext{Inbound: "in", User: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	entered, release, admitted, revoked := make(chan struct{}), make(chan struct{}), make(chan bool), make(chan struct{})
	go func() { admitted <- Admission(ctx, func() { close(entered); <-release }) }()
	<-entered
	go func() { owner.Revoke("in"); close(revoked) }()
	select {
	case <-revoked:
		t.Fatal("revoke escaped accepted callback")
	default:
	}
	close(release)
	if !<-admitted {
		t.Fatal("accepted callback lost")
	}
	<-revoked
	if Admission(ctx, func() { t.Fatal("retired callback entered") }) {
		t.Fatal("revoked epoch accepted")
	}
}

type observedConn struct {
	net.Conn
	closes atomic.Int32
}

func (c *observedConn) Close() error { c.closes.Add(1); return c.Conn.Close() }

func TestRetiredParentExClosesOnceWithoutRouting(t *testing.T) {
	owner := NewOwner()
	epoch := acceptedEpoch(t, owner, "in", 7)
	owner.Revoke("in")
	left, right := net.Pipe()
	defer func() { _ = right.Close() }()
	conn := &observedConn{Conn: left}
	closed := 0
	router := &boundRouter{epoch: epoch} // nil delegate must never be entered
	router.RouteConnectionEx(context.Background(), conn, adapter.InboundContext{Inbound: "in", User: "alice"}, N.CloseHandlerFunc(func(error) { closed++ }))
	if conn.closes.Load() != 1 || closed != 1 {
		t.Fatal("retired parent close boundary was not exactly once")
	}
}

func TestUnacceptedAndAmbiguousBindingsFailClosed(t *testing.T) {
	owner := NewOwner()
	_, epoch, err := owner.Prepare(nil, "in", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := epoch.bind(t.Context(), adapter.InboundContext{}); err == nil {
		t.Fatal("candidate admitted before publication")
	}
	_, _, err = owner.Prepare(nil, "in", []Binding{{Inbound: "in", Principal: "alice", ClientID: 1}, {Inbound: "in", Principal: "alice", ClientID: 2}})
	if err == nil {
		t.Fatal("ambiguous identity accepted")
	}
}
