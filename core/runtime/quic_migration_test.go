//go:build with_quic

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/inboundidentity"
)

// A loopback NAT fixture changes only the UDP socket between proxy and server.
// There is no host route, firewall, interface or transport-identity mutation.
type quicNATFixture struct {
	front     *net.UDPConn
	server    *net.UDPAddr
	mu        sync.Mutex
	client    *net.UDPAddr
	active    *net.UDPConn
	upstreams []*net.UDPConn
	workers   sync.WaitGroup
}

func newQUICNATFixture(t *testing.T, serverPort int) *quicNATFixture {
	t.Helper()
	front, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	nat := &quicNATFixture{front: front, server: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: serverPort}}
	nat.rebind(t)
	nat.workers.Add(1)
	go func() {
		defer nat.workers.Done()
		packet := make([]byte, 65536)
		for {
			n, client, err := front.ReadFromUDP(packet)
			if err != nil {
				return
			}
			nat.mu.Lock()
			nat.client = client
			upstream := nat.active
			nat.mu.Unlock()
			if _, err := upstream.WriteToUDP(packet[:n], nat.server); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = nat.front.Close()
		nat.mu.Lock()
		conns := append([]*net.UDPConn{}, nat.upstreams...)
		nat.mu.Unlock()
		for _, conn := range conns {
			_ = conn.Close()
		}
		done := make(chan struct{})
		go func() { nat.workers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("NAT fixture leaked")
		}
	})
	return nat
}

func (n *quicNATFixture) rebind(t *testing.T) int {
	t.Helper()
	upstream, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	n.active = upstream
	n.upstreams = append(n.upstreams, upstream)
	n.mu.Unlock()
	n.workers.Add(1)
	go func() {
		defer n.workers.Done()
		packet := make([]byte, 65536)
		for {
			count, _, err := upstream.ReadFromUDP(packet)
			if err != nil {
				return
			}
			n.mu.Lock()
			client := n.client
			n.mu.Unlock()
			if client != nil {
				if _, err := n.front.WriteToUDP(packet[:count], client); err != nil {
					return
				}
			}
		}
	}()
	return upstream.LocalAddr().(*net.UDPAddr).Port
}

func TestQUICParentIdentitySurvivesNATPortRebinding(t *testing.T) {
	for _, kind := range []string{"hysteria", "hysteria2", "tuic"} {
		t.Run(kind, func(t *testing.T) {
			f := newQUICFixture(t, kind)
			nat := newQUICNATFixture(t, f.port)
			peer, err := f.dialAt(t, 7, nat.front.LocalAddr().String())
			if err != nil {
				t.Fatal("NAT authenticated fixture:", err)
			}
			t.Cleanup(func() {
				if peer.transport != nil {
					_ = peer.transport.Close()
				}
				_ = peer.conn.CloseWithError(0, "")
			})
			before := f.parents(t, 7)
			if len(before.Parents) != 1 {
				t.Fatal("missing authenticated parent")
			}
			nat.mu.Lock()
			oldPort := nat.active.LocalAddr().(*net.UDPAddr).Port
			nat.mu.Unlock()
			newPort := nat.rebind(t)
			if newPort == oldPort {
				t.Fatal("fixture did not change UDP source port")
			}
			stream, err := peer.echoStream(t.Context(), f.echo.Addr().String())
			if err != nil {
				t.Fatal("authenticated rebinding echo:", err)
			}
			defer stream.CancelRead(0)
			defer stream.Close()
			after := f.parents(t, 7)
			if len(after.Parents) != 1 || before.Parents[0].QUICParentTarget != after.Parents[0].QUICParentTarget {
				t.Fatal("address migration confused parent identity")
			}
			migratedSource := false
			for _, flow := range after.Connections {
				_, port, _ := net.SplitHostPort(flow.Source)
				if port == strconv.Itoa(newPort) {
					migratedSource = true
				}
			}
			if !migratedSource {
				t.Fatal("server-side migrated source was not observed")
			}
			result, err := f.core.Disconnect(t.Context(), DisconnectRequest{Generation: before.Generation, ClientID: 7, Parents: []QUICParentTarget{before.Parents[0].QUICParentTarget}})
			if err != nil || result.ParentsClosed != 1 {
				t.Fatal("authenticated kick lost migrated handle")
			}
			assertQUICRemoteClose(t, peer)
			t.Log("real loopback NAT port rebinding + stable authenticated handle + remote selective close PASS")
		})
	}
}

func TestQUICPartialStartupAndConcurrentCloseOnce(t *testing.T) {
	for _, kind := range []string{"hysteria", "hysteria2", "tuic"} {
		t.Run(kind, func(t *testing.T) {
			f := newQUICFixture(t, kind)
			occupied, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := occupied.LocalAddr().(*net.UDPAddr).Port
			raw, _ := json.Marshal(f.inbound)
			var candidate map[string]any
			_ = json.Unmarshal(raw, &candidate)
			candidate["tag"], candidate["listen_port"] = "failed-quic-fixture", port
			raw, _ = json.Marshal(candidate)
			bindings := []inboundidentity.Binding{{Inbound: "failed-quic-fixture", Principal: "verified-7", ClientID: 7}, {Inbound: "failed-quic-fixture", Principal: "verified-9", ClientID: 9}}
			if err := f.core.AddInboundWithBindings(raw, bindings); err == nil {
				t.Fatal("colliding candidate published")
			}
			if _, active := f.core.instance.InboundIdentity().CurrentEpoch("failed-quic-fixture"); active {
				t.Fatal("failed identity epoch published")
			}
			_ = occupied.Close()
			if err := f.core.AddInboundWithBindings(raw, bindings); err != nil {
				t.Fatal("failed candidate leaked listener or lifecycle state:", err)
			}
			if err := f.core.RemoveInbound("failed-quic-fixture"); err != nil {
				t.Fatal(err)
			}
			freed, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err != nil {
				t.Fatal("partial startup/retry listener leaked")
			}
			_ = freed.Close()
			peer := f.peer(t, 7)
			inbound, _ := f.core.instance.Inbound().Get("quic-fixture")
			results := make(chan error, 16)
			start := make(chan struct{})
			for range 16 {
				go func() { <-start; results <- inbound.Close() }()
			}
			close(start)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			for range 16 {
				select {
				case err := <-results:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("concurrent close unbounded")
				}
			}
			assertQUICRemoteClose(t, peer)
			if err := f.core.RemoveInbound("quic-fixture"); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Fatal(err)
			}
		})
	}
}
