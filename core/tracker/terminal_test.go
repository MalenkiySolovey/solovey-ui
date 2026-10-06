package tracker

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
)

func TestRoutedWrappersFinishOnceAcrossTerminalIOAndClose(t *testing.T) {
	for _, owner := range []string{"connection", "stats"} {
		for _, transport := range []string{"tcp", "packet"} {
			for _, scenario := range []struct {
				name   string
				err    error
				active int64
			}{
				{"eof", io.EOF, 0},
				{"closed", net.ErrClosed, 0},
				{"timeout", &net.DNSError{IsTimeout: true}, 1},
			} {
				t.Run(owner+"/"+transport+"/"+scenario.name, func(t *testing.T) {
					var router adapter.ConnectionTracker
					var group *trackerWaitGroup
					if owner == "connection" {
						tracker := NewConnTracker()
						router, group = tracker, tracker.inflight
					} else {
						tracker := NewStatsTracker()
						router, group = tracker, tracker.inflight
					}
					var actions []func()
					if transport == "tcp" {
						conn := router.RoutedConnection(context.Background(), terminalTestConn{err: scenario.err}, adapter.InboundContext{Inbound: "in"}, nil, nil)
						actions = []func(){func() { _, _ = conn.Read(make([]byte, 1)) }, func() { _, _ = conn.Write([]byte("x")) }, func() { _ = conn.Close() }}
					} else {
						conn := router.RoutedPacketConnection(context.Background(), terminalTestPacketConn{err: scenario.err}, adapter.InboundContext{Inbound: "in"}, nil, nil)
						buffer := buf.As([]byte("x"))
						defer buffer.Release()
						actions = []func(){func() { _, _ = conn.ReadPacket(buffer) }, func() { _ = conn.WritePacket(buffer, M.Socksaddr{}) }, func() { _ = conn.Close() }}
					}
					actions[0]()
					if group.Active() != scenario.active {
						t.Fatalf("first I/O active=%d, want %d", group.Active(), scenario.active)
					}
					var concurrent sync.WaitGroup
					for i := 0; i < 16; i++ {
						for _, action := range actions {
							concurrent.Add(1)
							go func(action func()) { defer concurrent.Done(); action() }(action)
						}
					}
					concurrent.Wait()
					if group.Active() != 0 {
						t.Fatalf("terminal I/O and repeated Close must finish exactly once: active=%d", group.Active())
					}
				})
			}
		}
	}
}

type terminalTestConn struct {
	net.Conn
	err error
}

func (c terminalTestConn) Read([]byte) (int, error)  { return 0, c.err }
func (c terminalTestConn) Write([]byte) (int, error) { return 0, c.err }
func (terminalTestConn) Close() error                { return nil }

type terminalTestPacketConn struct{ err error }

func (c terminalTestPacketConn) ReadPacket(*buf.Buffer) (M.Socksaddr, error) {
	return M.Socksaddr{}, c.err
}
func (c terminalTestPacketConn) WritePacket(*buf.Buffer, M.Socksaddr) error { return c.err }
func (terminalTestPacketConn) Close() error                                 { return nil }
func (terminalTestPacketConn) LocalAddr() net.Addr                          { return &net.UDPAddr{} }
func (terminalTestPacketConn) SetDeadline(time.Time) error                  { return nil }
func (terminalTestPacketConn) SetReadDeadline(time.Time) error              { return nil }
func (terminalTestPacketConn) SetWriteDeadline(time.Time) error             { return nil }
