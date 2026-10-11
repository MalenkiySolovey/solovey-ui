package inboundidentity

import (
	"context"
	"encoding/binary"
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

// Observe completion of a FIN in the pinned v0.0.11 wire receiver. Its
// session/frame.go defines a seven-byte header and command 3 for FIN. The
// next Read starts after recvLoop has completed the stream's idle-parent
// hook, unlike Stream.Close returning after a competing close-once winner.
type parentFixtureConn struct {
	net.Conn
	header       [7]byte
	headerBytes  int
	bodyBytes    int
	finPending   bool
	finProcessed chan struct{}
}

func (c *parentFixtureConn) Read(p []byte) (int, error) {
	if c.finPending {
		c.finPending = false
		c.finProcessed <- struct{}{}
	}
	n, err := c.Conn.Read(p)
	for remaining := p[:n]; len(remaining) != 0; {
		if c.bodyBytes != 0 {
			used := min(c.bodyBytes, len(remaining))
			c.bodyBytes -= used
			remaining = remaining[used:]
			continue
		}
		used := copy(c.header[c.headerBytes:], remaining)
		c.headerBytes += used
		remaining = remaining[used:]
		if c.headerBytes == len(c.header) {
			c.bodyBytes = int(binary.BigEndian.Uint16(c.header[5:]))
			c.finPending = c.header[0] == 3 && c.bodyBytes == 0
			c.headerBytes = 0
		}
	}
	return n, err
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
	finProcessed := make(chan struct{}, 2)
	client, err := anytls.NewClient(t.Context(), anytls.ClientConfig{Password: "fixture-only", Logger: logger, MinIdleSession: 1, DialOut: func(ctx context.Context) (net.Conn, error) {
		dials.Add(1)
		listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			return nil, err
		}
		defer listener.Close()
		if err := listener.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			return nil, err
		}
		left, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp4", listener.Addr().String())
		if err != nil {
			return nil, err
		}
		right, err := listener.AcceptTCP()
		if err != nil {
			_ = left.Close()
			return nil, err
		}
		go func() {
			defer func() { _ = right.Close(); serverDone <- struct{}{} }()
			_ = server.NewConnection(t.Context(), right, M.SocksaddrFromNet(right.RemoteAddr()), nil)
		}()
		return &parentFixtureConn{Conn: left, finProcessed: finProcessed}, nil
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
		select {
		case <-finProcessed:
		case <-ctx.Done():
			t.Fatal("parent idle return barrier timed out")
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
		t.Fatalf("retired parent: dials=%d accepted=%d; want one authenticated parent and one admitted flow", dials.Load(), delegate.accepted.Load())
	}
}
