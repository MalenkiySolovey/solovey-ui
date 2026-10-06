package runtime

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/tracker"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
)

func TestCoreRestartFencesLateTrackerIOFromNewGeneration(t *testing.T) {
	for _, transport := range []string{"tcp", "packet"} {
		t.Run(transport, func(t *testing.T) {
			core := NewCore()
			config := []byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`)
			if err := core.Start(config); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = core.Stop() })
			oldStats, oldConnections, oldGeneration := core.statsTracker, core.connTracker, core.managerGeneration
			oldIO := newLateTrackerIO()
			t.Cleanup(oldIO.releaseWrite)
			oldWrite, oldClose := routedTrackerIO(t, transport, oldStats, oldConnections, oldIO)
			completed := make(chan error, 1)
			go func() { completed <- oldWrite() }()
			select {
			case <-oldIO.started:
			case <-time.After(3 * time.Second):
				t.Fatal("old routed I/O did not start")
			}
			// A transport can report an already completed I/O after Close. Hold
			// that completion across an actual Stop/Start to test its counter owner.
			if err := oldClose(); err != nil {
				t.Fatal(err)
			}
			if err := core.Stop(); err != nil {
				t.Fatal(err)
			}
			if err := core.Start(config); err != nil {
				t.Fatal(err)
			}
			if core.statsTracker == oldStats || core.connTracker == oldConnections || core.managerGeneration <= oldGeneration {
				t.Fatal("restart reused the old tracker generation")
			}
			newIO := newLateTrackerIO()
			newIO.releaseWrite()
			newWrite, newClose := routedTrackerIO(t, transport, core.statsTracker, core.connTracker, newIO)
			t.Cleanup(func() { _ = newClose() })
			oldIO.releaseWrite()
			select {
			case err := <-completed:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("old routed I/O did not complete")
			}
			if traffic := trackerInboundTraffic(oldStats.GetStats()); traffic != 7 {
				t.Fatalf("late completion did not exercise old counters: traffic=%d", traffic)
			}
			if traffic := trackerInboundTraffic(core.statsTracker.GetStats()); traffic != 0 {
				t.Fatalf("old completion changed the new counters: traffic=%d", traffic)
			}
			if err := newWrite(); err != nil {
				t.Fatal(err)
			}
			if traffic := trackerInboundTraffic(core.statsTracker.GetStats()); traffic != 7 {
				t.Fatalf("new traffic was not counted in its generation: traffic=%d", traffic)
			}
			_ = oldClose()
			if count := core.connTracker.CloseConnByInbound("in"); count != 1 {
				t.Fatalf("old completion disturbed current tracking: active connections=%d", count)
			}
		})
	}
}

func routedTrackerIO(t *testing.T, transport string, stats *tracker.StatsTracker, connections *tracker.ConnTracker, state *lateTrackerIO) (func() error, func() error) {
	t.Helper()
	metadata := adapter.InboundContext{Inbound: "in"}
	if transport == "tcp" {
		conn := stats.RoutedConnection(context.Background(), lateTrackerConn{state: state}, metadata, nil, nil)
		conn = connections.RoutedConnection(context.Background(), conn, metadata, nil, nil)
		return func() error { _, err := conn.Write([]byte("traffic")); return err }, conn.Close
	}
	conn := stats.RoutedPacketConnection(context.Background(), lateTrackerPacketConn{state: state}, metadata, nil, nil)
	conn = connections.RoutedPacketConnection(context.Background(), conn, metadata, nil, nil)
	return func() error {
		buffer := buf.As([]byte("traffic"))
		defer buffer.Release()
		return conn.WritePacket(buffer, M.Socksaddr{})
	}, conn.Close
}

func trackerInboundTraffic(stats []tracker.Stat) int64 {
	var total int64
	for _, stat := range stats {
		if stat.Resource == "inbound" && stat.Tag == "in" {
			total += stat.Traffic
		}
	}
	return total
}

type lateTrackerIO struct {
	started, released      chan struct{}
	startOnce, releaseOnce sync.Once
}

func newLateTrackerIO() *lateTrackerIO {
	return &lateTrackerIO{started: make(chan struct{}), released: make(chan struct{})}
}
func (s *lateTrackerIO) write() {
	s.startOnce.Do(func() { close(s.started) })
	<-s.released
}
func (s *lateTrackerIO) releaseWrite() { s.releaseOnce.Do(func() { close(s.released) }) }

type lateTrackerConn struct {
	net.Conn
	state *lateTrackerIO
}

func (c lateTrackerConn) Read([]byte) (int, error)    { return 0, io.EOF }
func (c lateTrackerConn) Write(b []byte) (int, error) { c.state.write(); return len(b), nil }
func (lateTrackerConn) Close() error                  { return nil }

type lateTrackerPacketConn struct {
	state *lateTrackerIO
}

func (lateTrackerPacketConn) ReadPacket(*buf.Buffer) (M.Socksaddr, error) {
	return M.Socksaddr{}, io.EOF
}
func (c lateTrackerPacketConn) WritePacket(*buf.Buffer, M.Socksaddr) error {
	c.state.write()
	return nil
}
func (lateTrackerPacketConn) Close() error                     { return nil }
func (lateTrackerPacketConn) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (lateTrackerPacketConn) SetDeadline(time.Time) error      { return nil }
func (lateTrackerPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (lateTrackerPacketConn) SetWriteDeadline(time.Time) error { return nil }
