package inboundidentity

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	anytls "github.com/anytls/sing-anytls"
	"github.com/anytls/sing-anytls/padding"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common/auth"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type parentFixtureRouter struct {
	adapter.Router
	accepted atomic.Int32
}

func (r *parentFixtureRouter) RouteConnectionEx(_ context.Context, conn net.Conn, _ adapter.InboundContext, _ N.CloseHandlerFunc) {
	r.accepted.Add(1)
	_ = conn.Close()
}

type parentFixtureHandler struct {
	router     adapter.Router
	attempted  chan string
	allowRoute chan struct{}
	routed     chan struct{}
}

func (h *parentFixtureHandler) NewConnectionEx(ctx context.Context, conn net.Conn, source, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	user, _ := auth.UserFromContext[string](ctx)
	h.attempted <- user
	<-h.allowRoute
	h.router.RouteConnectionEx(ctx, conn, adapter.InboundContext{Inbound: "any", InboundType: "anytls", User: user, Source: source, Destination: destination}, onClose)
	h.routed <- struct{}{}
}

// Exercise the pinned authenticated parent protocol, rather than an invented
// IP fixture: a pooled AnyTLS parent survives flow close and reopens a stream.
func TestAnyTLSAuthenticatedParentCannotReopenRetiredInbound(t *testing.T) {
	owner := NewOwner()
	delegate := &parentFixtureRouter{}
	router, epoch, err := owner.Prepare(delegate, "any", []Binding{{Inbound: "any", Principal: "authenticated", ClientID: 7}})
	if err != nil {
		t.Fatal(err)
	}
	owner.Publish("any", epoch)
	handler := &parentFixtureHandler{router: router, attempted: make(chan string, 2), allowRoute: make(chan struct{}), routed: make(chan struct{}, 2)}
	logger := log.NewNOPFactory().Logger()
	server, err := anytls.NewService(anytls.ServiceConfig{PaddingScheme: padding.DefaultPaddingScheme, Users: []anytls.User{{Name: "authenticated", Password: "fixture-only"}}, Handler: handler, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	var dials atomic.Int32
	serverDone := make(chan struct{}, 4)
	client, err := anytls.NewClient(t.Context(), anytls.ClientConfig{Password: "fixture-only", Logger: logger, MinIdleSession: 1, DialOut: func(ctx context.Context) (net.Conn, error) {
		dials.Add(1)
		left, right := net.Pipe()
		go func() {
			defer func() { _ = right.Close(); serverDone <- struct{}{} }()
			_ = server.NewConnection(t.Context(), right, M.ParseSocksaddr("127.0.0.1:1234"), nil)
		}()
		return left, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		select {
		case <-serverDone:
		case <-time.After(3 * time.Second):
			t.Error("parent fixture did not close")
		}
	})
	open := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		conn, err := client.CreateProxy(ctx, M.ParseSocksaddr("fixture.invalid:443"))
		if err != nil {
			t.Fatal(err)
		}
		// Flush the destination before permitting server-side close. The pinned
		// client Stream has an unrelated dieErr race on concurrent Write/remote
		// close; this fixture qualifies parent admission with ordered protocol IO.
		_, _ = conn.Write(nil)
		select {
		case user := <-handler.attempted:
			if user != "authenticated" {
				t.Fatal("missing authenticated parent principal")
			}
		case <-ctx.Done():
			t.Fatal("parent stream barrier timed out")
		}
		handler.allowRoute <- struct{}{}
		select {
		case <-handler.routed:
		case <-ctx.Done():
			t.Fatal("parent route barrier timed out")
		}
		_ = conn.Close()
	}
	open()
	if delegate.accepted.Load() != 1 {
		t.Fatal("initial authenticated flow was not routed")
	}
	owner.Revoke("any")
	open()
	if dials.Load() != 1 || delegate.accepted.Load() != 1 {
		t.Fatal("retired parent reauthenticated or admitted another flow")
	}
}
