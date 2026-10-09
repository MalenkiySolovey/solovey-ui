package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/inboundidentity"
	"github.com/MalenkiySolovey/solovey-ui/core/tracker"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/daemon"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type projectionRouter struct {
	adapter.Router
	routed *tracker.RoutedTracker
	tcp    net.Conn
	udp    N.PacketConn
}

func (r *projectionRouter) RouteConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	r.tcp = r.routed.RoutedConnection(ctx, conn, metadata, nil, nil)
	return nil
}
func (r *projectionRouter) RoutePacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	r.udp = r.routed.RoutedPacketConnection(ctx, conn, metadata, nil, nil)
	return nil
}

type countedFlow struct {
	net.Conn
	closes atomic.Int32
}

func (c *countedFlow) Close() error { c.closes.Add(1); return c.Conn.Close() }

type countedPacket struct {
	N.PacketConn
	closes atomic.Int32
}

func (c *countedPacket) Close() error { c.closes.Add(1); return nil }

func fixtureIdentityRouter(t *testing.T, c *Core, tag string, clientID uint) (adapter.Router, *projectionRouter) {
	t.Helper()
	delegate := &projectionRouter{routed: tracker.NewRoutedTracker(c.instance.StatsTracker(), c.instance.ConnTracker())}
	router, epoch, err := c.instance.InboundIdentity().Prepare(delegate, tag, []inboundidentity.Binding{{Inbound: tag, Principal: "authenticated", ClientID: clientID}})
	if err != nil {
		t.Fatal(err)
	}
	c.instance.InboundIdentity().Publish(tag, epoch)
	return router, delegate
}

func TestOfficialLiveProjectionAndVerifiedDisconnect(t *testing.T) {
	for _, kind := range []string{"anytls", "snell"} {
		t.Run(kind, func(t *testing.T) { testOfficialProtocolProjection(t, kind) })
	}
}

func testOfficialProtocolProjection(t *testing.T, kind string) {
	c := startPrivateFixture(t)
	generation := c.privateAPI.generation
	empty, err := c.Connections(t.Context(), generation, 7, 100)
	if err != nil || empty.Total != 0 || empty.Generation != generation {
		t.Fatal("invalid zero projection")
	}
	router, delegate := fixtureIdentityRouter(t, c, "in", 7)
	metadata := adapter.InboundContext{Inbound: "in", InboundType: kind, User: "authenticated", Network: "tcp", Source: M.ParseSocksaddr("127.0.0.1:12345"), Destination: M.ParseSocksaddr("example.invalid:443")}
	left, right := net.Pipe()
	t.Cleanup(func() { _ = right.Close() })
	raw := &countedFlow{Conn: left}
	if err := router.RouteConnection(t.Context(), raw, metadata); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = delegate.tcp.Close() })
	packet := &countedPacket{}
	metadata.Network = "udp"
	if err := router.RoutePacketConnection(t.Context(), packet, metadata); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = delegate.udp.Close() })
	// Same principal text without the inbound callback proof remains unknown.
	uLeft, uRight := net.Pipe()
	unknown := c.instance.ConnTracker().RoutedConnection(t.Context(), uLeft, metadata, nil, nil)
	t.Cleanup(func() { _ = unknown.Close(); _ = uRight.Close() })
	snapshot, err := c.Connections(t.Context(), generation, 7, 1)
	if err != nil || snapshot.Total != 2 || snapshot.ActualTotal != 3 || snapshot.Unassociated != 1 || !snapshot.Truncated || len(snapshot.Connections) != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), "authenticated") || strings.Contains(string(encoded), "__solovey_principal") {
		t.Fatal("private identity leaked")
	}
	other, err := c.Connections(t.Context(), generation, 9, 100)
	if err != nil || other.Total != 0 {
		t.Fatal("cross-client projection")
	}
	result, err := c.Disconnect(t.Context(), DisconnectRequest{Generation: generation, ClientID: 7})
	if err != nil || result.Closed != 2 || result.Matched != 2 || result.Outcome != "PARTIAL_DISCONNECT" || result.ParentClosed {
		t.Fatalf("disconnect=%+v err=%v", result, err)
	}
	if raw.closes.Load() != 1 || packet.closes.Load() != 1 {
		t.Fatal("not close-once")
	}
	remaining, err := c.Connections(t.Context(), generation, 0, 100)
	if err != nil || remaining.Total != 1 || remaining.Connections[0].Identity != "unavailable" {
		t.Fatal("closed history or unrelated flow affected")
	}
	result, err = c.Disconnect(t.Context(), DisconnectRequest{Generation: generation, FlowID: remaining.Connections[0].ID})
	if err != nil || result.Outcome != "FLOW_CLOSED" || result.Closed != 1 {
		t.Fatal("single flow close")
	}
	result, err = c.Disconnect(t.Context(), DisconnectRequest{Generation: generation, ClientID: 7})
	if err != nil || result.Outcome != "ALREADY_GONE" || result.ParentClosed {
		t.Fatal("zero flows claimed parent closure")
	}
	c.instance.InboundIdentity().Revoke("in")
	if err := router.RoutePacketConnection(t.Context(), &countedPacket{}, metadata); err == nil {
		t.Fatal("retired parent admitted a stream")
	}
}

type connectionFrameStream struct {
	grpc.ClientStream
	frame *daemon.ConnectionEvents
}

func (s *connectionFrameStream) Recv() (*daemon.ConnectionEvents, error) { return s.frame, nil }

type snapshotAPI struct {
	daemon.StartedServiceClient
	frame            *daemon.ConnectionEvents
	closes           atomic.Int32
	entered, release chan struct{}
}

func (s *snapshotAPI) SubscribeConnections(ctx context.Context, _ *daemon.SubscribeConnectionsRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[daemon.ConnectionEvents], error) {
	if s.entered != nil {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &connectionFrameStream{frame: s.frame}, nil
}
func (s *snapshotAPI) CloseConnection(context.Context, *daemon.CloseConnectionRequest, ...grpc.CallOption) (*emptypb.Empty, error) {
	s.closes.Add(1)
	return &emptypb.Empty{}, nil
}

func TestSnapshotBoundsClosedHistoryAndAcknowledgement(t *testing.T) {
	c := startPrivateFixture(t)
	generation := c.privateAPI.generation
	fake := &snapshotAPI{frame: &daemon.ConnectionEvents{Reset_: true, Events: []*daemon.ConnectionEvent{
		{Connection: &daemon.Connection{Id: "closed", CreatedAt: 1}}, {Connection: &daemon.Connection{Id: "closed", ClosedAt: 2}}, {Connection: &daemon.Connection{Id: "live", CreatedAt: 3}},
	}}}
	c.privateAPI.rpc = fake
	snapshot, err := c.Connections(t.Context(), generation, 0, 100)
	if err != nil || snapshot.Total != 1 || snapshot.Connections[0].ID != "live" {
		t.Fatal("closed duplicate retained")
	}
	result, err := c.Disconnect(t.Context(), DisconnectRequest{Generation: generation, FlowID: "live"})
	if err != nil || result.Outcome != "PARTIAL_DISCONNECT" || result.Closed != 0 || result.Remaining != 1 {
		t.Fatal("RPC acknowledgement claimed closure")
	}
	fake.frame.Events = make([]*daemon.ConnectionEvent, maxConnectionFrameEvents+1)
	if _, err := c.Connections(t.Context(), generation, 0, 100); !errors.Is(err, ErrRuntimeLimit) {
		t.Fatal("unbounded initial frame")
	}
	if _, err := c.Connections(t.Context(), generation, 0, 257); !errors.Is(err, ErrRuntimeLimit) {
		t.Fatal("unbounded projection")
	}
	c.snapshotReaders.Store(8)
	if _, err := c.Connections(t.Context(), generation, 0, 100); !errors.Is(err, ErrRuntimeLimit) {
		t.Fatal("unbounded readers")
	}
	c.snapshotReaders.Store(0)
	if _, err := c.Disconnect(t.Context(), DisconnectRequest{Generation: "old", FlowID: "live"}); !errors.Is(err, ErrStaleGeneration) || fake.closes.Load() != 1 {
		t.Fatal("stale disconnect called RPC")
	}
}

func TestConnectionSnapshotLeaseFencesRestart(t *testing.T) {
	c := startPrivateFixture(t)
	old := c.privateAPI.generation
	fake := &snapshotAPI{frame: &daemon.ConnectionEvents{Reset_: true}, entered: make(chan struct{}), release: make(chan struct{})}
	c.privateAPI.rpc = fake
	read := make(chan error, 1)
	go func() { _, err := c.Connections(t.Context(), old, 0, 100); read <- err }()
	waitRuntimeBarrier(t, fake.entered)
	replace := make(chan error, 1)
	go func() {
		if err := c.Stop(); err != nil {
			replace <- err
			return
		}
		replace <- c.Start([]byte(privateFixture))
	}()
	close(fake.release)
	if err := <-read; err != nil {
		t.Fatal(err)
	}
	if err := <-replace; err != nil {
		t.Fatal(err)
	}
	if _, err := c.Connections(t.Context(), old, 0, 100); !errors.Is(err, ErrStaleGeneration) {
		t.Fatal("late read accepted replacement")
	}
}

type rejectingPrincipalObserver struct {
	calls     int
	principal string
}

func (o *rejectingPrincipalObserver) ObserveAndAllow(principal, _ string) bool {
	o.calls++
	o.principal = principal
	return false
}

func TestIdentityDoesNotBypassTCPUDPAdmissionOrAccounting(t *testing.T) {
	for _, kind := range []string{"anytls", "snell"} {
		t.Run(kind, func(t *testing.T) { testAuthenticatedProtocolAdmission(t, kind) })
	}
}

func testAuthenticatedProtocolAdmission(t *testing.T, kind string) {
	observer := &rejectingPrincipalObserver{}
	c := NewCore(observer)
	if err := c.Start([]byte(privateFixture)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Stop() })
	router, delegate := fixtureIdentityRouter(t, c, "in", 7)
	metadata := adapter.InboundContext{Inbound: "in", InboundType: kind, User: "authenticated", Source: M.ParseSocksaddr("127.0.0.1:1234")}
	left, right := net.Pipe()
	defer func() { _ = right.Close() }()
	raw := &countedFlow{Conn: left}
	if err := router.RouteConnection(t.Context(), raw, metadata); err != nil {
		t.Fatal(err)
	}
	packet := &countedPacket{}
	if err := router.RoutePacketConnection(t.Context(), packet, metadata); err != nil {
		t.Fatal(err)
	}
	if delegate.tcp != raw || delegate.udp != packet {
		t.Fatal("rejected connection contract changed")
	}
	snapshot, err := c.Connections(t.Context(), c.privateAPI.generation, 0, 100)
	if err != nil || snapshot.Total != 0 || len(c.instance.StatsTracker().GetStats()) != 0 {
		t.Fatal("rejected flow entered accounting or inventory")
	}
	if observer.calls != 2 || observer.principal != "authenticated" || raw.closes.Load() != 1 || packet.closes.Load() != 1 {
		t.Fatal("admission principal/close-once changed")
	}
}

type failingHotInbound struct {
	tag      string
	listener net.Listener
	closes   int
	router   adapter.Router
}

func (i *failingHotInbound) Type() string { return "hot-fixture" }
func (i *failingHotInbound) Tag() string  { return i.tag }
func (i *failingHotInbound) Start(stage adapter.StartStage) error {
	if stage == adapter.StartStateStart {
		var err error
		i.listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		return errors.New("fixture start failure")
	}
	return nil
}
func (i *failingHotInbound) Close() error {
	i.closes++
	if i.listener != nil {
		return i.listener.Close()
	}
	return nil
}

func TestHotInboundStartFailureClosesCandidateWithoutPublication(t *testing.T) {
	c := startPrivateFixture(t)
	generation := c.privateAPI.generation
	var candidate *failingHotInbound
	registry := service.FromContext[adapter.InboundRegistry](c.instance.Context()).(*inbound.Registry)
	inbound.Register[option.ListenOptions](registry, "hot-fixture", func(_ context.Context, router adapter.Router, _ log.ContextLogger, tag string, _ option.ListenOptions) (adapter.Inbound, error) {
		candidate = &failingHotInbound{tag: tag, router: router}
		return candidate, nil
	})
	if err := c.AddInboundWithBindings([]byte(`{"type":"hot-fixture","tag":"failed"}`), []inboundidentity.Binding{{Inbound: "failed", Principal: "authenticated", ClientID: 7}}); err == nil {
		t.Fatal("failed candidate accepted")
	}
	if candidate == nil || candidate.closes != 1 {
		t.Fatal("unpublished hot candidate leaked")
	}
	if _, found := c.inboundManager.Get("failed"); found {
		t.Fatal("failed candidate published")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	left, right := net.Pipe()
	defer func() { _ = left.Close(); _ = right.Close() }()
	if candidate.router.RouteConnection(ctx, left, adapter.InboundContext{Inbound: "failed", User: "authenticated"}) == nil {
		t.Fatal("failed candidate admitted identity")
	}
	rebound, err := net.Listen("tcp", candidate.listener.Addr().String())
	if err != nil {
		t.Fatal("candidate listener remained occupied")
	}
	_ = rebound.Close()
	if c.RuntimeStatus(t.Context()).Generation != generation {
		t.Fatal("hot failure replaced accepted core")
	}
}
