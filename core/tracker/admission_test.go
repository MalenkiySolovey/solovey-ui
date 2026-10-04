package tracker

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
)

type atomicAdmissionObserver struct {
	allow          bool
	calls          int
	client, source string
}

func (o *atomicAdmissionObserver) ObserveAndAllow(client, source string) bool {
	o.calls++
	o.client = client
	o.source = source
	return o.allow
}

// Keep obsolete methods on this fake so any accidental second observation
// or fallback to the old contract fails the adapter regression immediately.
func (*atomicAdmissionObserver) Allow(string, string) bool { panic("split admission used") }
func (*atomicAdmissionObserver) Record(string, string)     { panic("duplicate observation used") }

type admissionPacketConn struct{ closed bool }

func (c *admissionPacketConn) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	if c.closed {
		return M.Socksaddr{}, net.ErrClosed
	}
	_, err := buffer.Write([]byte("read"))
	return M.ParseSocksaddr("198.51.100.2:1234"), err
}
func (c *admissionPacketConn) WritePacket(*buf.Buffer, M.Socksaddr) error {
	if c.closed {
		return net.ErrClosed
	}
	return nil
}
func (c *admissionPacketConn) Close() error                   { c.closed = true; return nil }
func (*admissionPacketConn) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (*admissionPacketConn) SetDeadline(time.Time) error      { return nil }
func (*admissionPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (*admissionPacketConn) SetWriteDeadline(time.Time) error { return nil }

func TestStatsTrackerAtomicAdmissionPaths(t *testing.T) {
	for _, path := range []string{"tcp", "packet"} {
		for _, allow := range []bool{false, true} {
			name := path + "/reject"
			if allow {
				name = path + "/accept"
			}
			t.Run(name, func(t *testing.T) {
				observer := &atomicAdmissionObserver{allow: allow}
				tracker := NewStatsTracker(observer)
				metadata := adapter.InboundContext{Inbound: "in", User: "alice", Source: M.ParseSocksaddr("198.51.100.1:4321")}
				if path == "tcp" {
					raw := newBlockingTestConn()
					conn := tracker.RoutedConnection(context.Background(), raw, metadata, nil, fakeStatsOutbound{tag: "out"})
					if allow {
						defer conn.Close()
						if tracker.inflight.Active() != 1 {
							t.Fatal("accepted TCP lifecycle not tracked")
						}
						if _, err := conn.Write([]byte("sent")); err != nil {
							t.Fatal(err)
						}
						if err := conn.Close(); err != nil {
							t.Fatal(err)
						}
					} else {
						if conn != raw {
							t.Fatal("rejection replaced original TCP connection")
						}
						select {
						case <-raw.closed:
						default:
							t.Fatal("rejected TCP connection remains open")
						}
					}
				} else {
					raw := &admissionPacketConn{}
					conn := tracker.RoutedPacketConnection(context.Background(), raw, metadata, nil, fakeStatsOutbound{tag: "out"})
					if allow {
						defer conn.Close()
						if tracker.inflight.Active() != 1 {
							t.Fatal("accepted packet lifecycle not tracked")
						}
						buffer := buf.As([]byte("sent"))
						defer buffer.Release()
						if err := conn.WritePacket(buffer, M.ParseSocksaddr("198.51.100.2:1234")); err != nil {
							t.Fatal(err)
						}
						if err := conn.Close(); err != nil {
							t.Fatal(err)
						}
					} else if conn != raw || !raw.closed {
						t.Fatal("rejected packet connection remains open or was replaced")
					}
				}
				if observer.calls != 1 || observer.client != "alice" || observer.source != "198.51.100.1" {
					t.Fatalf("atomic observer call contract: %#v", observer)
				}
				if tracker.inflight.Active() != 0 {
					t.Fatal("connection leaked inflight accounting")
				}
				stats := tracker.GetStats()
				if !allow {
					if len(stats) != 0 || len(tracker.inbounds) != 0 || len(tracker.outbounds) != 0 || len(tracker.users) != 0 {
						t.Fatal("rejection entered accepted stats path")
					}
				} else {
					if len(stats) != 6 {
						t.Fatalf("accepted stats count=%d", len(stats))
					}
					for _, stat := range stats {
						expected := int64(4)
						if stat.Direction {
							expected = 0
						}
						if stat.Traffic != expected {
							t.Fatalf("accepted traffic accounting changed: %#v", stat)
						}
					}
				}
			})
		}
	}
}
