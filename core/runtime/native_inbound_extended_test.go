//go:build shtorm_extended && with_quic

package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/mieru"
	"github.com/sagernet/sing-box/protocol/trusttunnel"
	M "github.com/sagernet/sing/common/metadata"
)

// Clients are qualification adapters over the exact selected provider. They do
// not register production outbounds or substitute another engine at runtime.
func nativeInboundPeer(t *testing.T, f *quicFixture, transport string, user uint, password string) adapter.Outbound {
	t.Helper()
	ctx := f.core.instance.Context()
	logger := log.NewNOPFactory().Logger()
	var client adapter.Outbound
	var err error
	if f.kind == "mieru" {
		client, err = mieru.NewOutbound(ctx, f.core.instance.Router(), logger, "qualification-client", option.MieruOutboundOptions{
			ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: uint16(f.port)},
			Transport:     transport, UserName: fmt.Sprintf("verified-%d", user), Password: password, MTU: 1400,
		})
	} else {
		tls := f.inbound["tls"].(map[string]any)
		client, err = trusttunnel.NewOutbound(ctx, f.core.instance.Router(), logger, "qualification-client", option.TrustTunnelOutboundOptions{
			ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: uint16(f.port)},
			Username:      fmt.Sprintf("verified-%d", user), Password: password, QUIC: transport == "udp",
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: &option.OutboundTLSOptions{
				Enabled: true, ServerName: "quic-fixture.invalid", Certificate: tls["certificate"].([]string),
			}},
		})
	}
	if err != nil {
		t.Fatal("pinned qualification client construction failed", err)
	}
	lifecycle, ok := client.(adapter.Lifecycle)
	if !ok {
		t.Fatal("pinned qualification client has no native lifecycle")
	}
	scope := adapter.NewScope(ctx, logger)
	t.Cleanup(func() {
		if err := scope.Close(); err != nil {
			t.Error("qualification client cleanup failed", err)
		}
	})
	for _, stage := range adapter.ListStartStages {
		if err := scope.Start("qualification-client", lifecycle, stage); err != nil {
			t.Fatal("pinned qualification client start failed", err)
		}
	}
	return client
}

func nativeTCPWitness(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	var access sync.Mutex
	var conns []net.Conn
	accepted := make(chan struct{})
	workers.Add(1)
	go func() {
		defer workers.Done()
		defer close(accepted)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			access.Lock()
			conns = append(conns, conn)
			access.Unlock()
			workers.Add(1)
			go func() { defer workers.Done(); defer conn.Close(); _, _ = io.Copy(conn, conn) }()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-accepted:
		case <-time.After(2 * time.Second):
			t.Error("native traffic witness accept loop did not stop")
		}
		access.Lock()
		for _, conn := range conns {
			_ = conn.Close()
		}
		access.Unlock()
		done := make(chan struct{})
		go func() { workers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("native traffic witness did not drain")
		}
	})
	return listener
}

func newNativeMieruFixture(t *testing.T, transport string) *quicFixture {
	t.Helper()
	f := &quicFixture{kind: "mieru", core: NewCore(), port: quicPort(t)}
	f.inbound = map[string]any{"type": "mieru", "tag": "quic-fixture", "listen": "127.0.0.1", "listen_port": f.port,
		"transport": transport, "user_hint_is_mandatory": true, "mtu": 1400}
	f.users(7, 9)
	if err := f.core.StartWithBindings(f.raw(), quicBindings(7, 9)); err != nil {
		t.Fatal("native Mieru startup failed", err)
	}
	t.Cleanup(func() {
		if err := f.core.Stop(); err != nil {
			t.Error("native Mieru stop failed", err)
		}
	})
	f.echo = nativeTCPWitness(t)
	return f
}

func nativeEcho(conn net.Conn) error {
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte("native-loopback")); err != nil {
		return err
	}
	response := make([]byte, len("native-loopback"))
	if _, err := io.ReadFull(conn, response); err != nil {
		return err
	}
	if !bytes.Equal(response, []byte("native-loopback")) {
		return fmt.Errorf("native loopback response mismatch")
	}
	return nil
}

func nativeStream(t *testing.T, peer adapter.Outbound, f *quicFixture) net.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := peer.DialContext(ctx, "tcp", M.ParseSocksaddr(f.echo.Addr().String()))
	if err != nil {
		t.Fatal("native authenticated stream failed", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := nativeEcho(conn); err != nil {
		t.Fatal("native authenticated data plane failed", err)
	}
	return conn
}

func nativeUDPWitness(t *testing.T, peer adapter.Outbound) {
	t.Helper()
	echo, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		data := make([]byte, 128)
		n, addr, err := echo.ReadFrom(data)
		if err == nil {
			_, _ = echo.WriteTo(data[:n], addr)
		}
	}()
	t.Cleanup(func() { _ = echo.Close(); <-done })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	packet, err := peer.ListenPacket(ctx, M.ParseSocksaddr(echo.LocalAddr().String()))
	if err != nil {
		t.Fatal("native authenticated packet setup failed", err)
	}
	defer packet.Close()
	_ = packet.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := packet.WriteTo([]byte("native-udp"), echo.LocalAddr()); err != nil {
		t.Fatal("native authenticated packet write failed", err)
	}
	response := make([]byte, 128)
	n, _, err := packet.ReadFrom(response)
	if err != nil || string(response[:n]) != "native-udp" {
		t.Fatal("native authenticated UDP data plane failed", err)
	}
}

func TestNativeInboundAuthenticatedDataPlaneAndIsolation(t *testing.T) {
	for _, fixture := range []struct{ kind, transport string }{{"mieru", "TCP"}, {"mieru", "UDP"}, {"trusttunnel", "tcp"}, {"trusttunnel", "udp"}} {
		t.Run(fixture.kind+"/"+fixture.transport, func(t *testing.T) {
			var f *quicFixture
			if fixture.kind == "mieru" {
				f = newNativeMieruFixture(t, fixture.transport)
			} else {
				f = newQUICFixture(t, fixture.kind)
			}
			first := nativeInboundPeer(t, f, fixture.transport, 7, quicFixturePassword)
			other := nativeInboundPeer(t, f, fixture.transport, 9, quicFixturePassword)
			firstStream, otherStream := nativeStream(t, first, f), nativeStream(t, other, f)
			nativeUDPWitness(t, first)
			snapshot := f.parents(t, 7)
			var firstID string
			for _, flow := range snapshot.Connections {
				if flow.ClientID != 7 || flow.InboundType != fixture.kind || flow.Identity != "verified" {
					t.Fatal("actual native authentication did not bind its Users identity")
				}
				if flow.Network == "tcp" {
					firstID = flow.ID
					if flow.Upload == 0 || flow.Download == 0 {
						t.Fatal("native data-plane traffic did not reach the existing counters")
					}
				}
			}
			if firstID == "" || snapshot.ParentTotal != 0 {
				t.Fatal("native flow is missing or unqualified parent control was advertised")
			}
			result, err := f.core.Disconnect(t.Context(), DisconnectRequest{Generation: snapshot.Generation, ClientID: 7, FlowID: firstID})
			if err != nil || result.Closed != 1 || result.ParentClosed {
				t.Fatal("native flow disconnect failed or overclaimed parent closure", err)
			}
			if nativeEcho(firstStream) == nil {
				t.Fatal("disconnected native flow kept forwarding")
			}
			if err := nativeEcho(otherStream); err != nil {
				t.Fatal("native disconnect affected another user", err)
			}
			if err := f.replace(9); err != nil {
				t.Fatal("native owner reload failed", err)
			}
			revoked := nativeInboundPeer(t, f, fixture.transport, 7, quicFixturePassword)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			conn, err := revoked.DialContext(ctx, "tcp", M.ParseSocksaddr(f.echo.Addr().String()))
			if err == nil {
				defer conn.Close()
				if nativeEcho(conn) == nil {
					t.Fatal("revoked native user forwarded through a fresh authenticated session")
				}
			}
			remaining := nativeInboundPeer(t, f, fixture.transport, 9, quicFixturePassword)
			_ = nativeStream(t, remaining, f)
			if got := f.parents(t, 7); got.Total != 0 {
				t.Fatal("revoked native user retained an admitted flow")
			}
		})
	}
}

func TestNativeInboundFinalUserRevocationAndReenable(t *testing.T) {
	for _, fixture := range []struct{ kind, transport string }{{"mieru", "TCP"}, {"mieru", "UDP"}, {"trusttunnel", "tcp"}, {"trusttunnel", "udp"}} {
		t.Run(fixture.kind+"/"+fixture.transport, func(t *testing.T) {
			var f *quicFixture
			if fixture.kind == "mieru" {
				f = newNativeMieruFixture(t, fixture.transport)
			} else {
				f = newQUICFixture(t, fixture.kind)
			}
			first := nativeInboundPeer(t, f, fixture.transport, 7, quicFixturePassword)
			other := nativeInboundPeer(t, f, fixture.transport, 9, quicFixturePassword)
			streams := []net.Conn{nativeStream(t, first, f), nativeStream(t, other, f)}
			replace := func(users ...uint) {
				t.Helper()
				// Keep the saved listener address and port through disable/re-enable.
				f.users(users...)
				raw, err := json.Marshal(f.inbound)
				if err != nil {
					t.Fatal(err)
				}
				// Match the existing Inbounds.Restart owner: retire the old
				// listener and its flows before binding the same saved port.
				if err := f.core.RemoveInbound("quic-fixture"); err != nil {
					t.Fatal("native owner removal failed", err)
				}
				f.core.CloseInboundConnections("quic-fixture")
				if err := f.core.AddInboundWithBindings(raw, quicBindings(users...)); err != nil {
					t.Fatal("native user replacement failed", err)
				}
			}
			replace()
			for _, stream := range streams {
				if nativeEcho(stream) == nil {
					t.Fatal("last-user revocation left an established flow forwarding")
				}
			}
			if fixture.kind == "mieru" {
				var occupied io.Closer
				var err error
				address := fmt.Sprintf("127.0.0.1:%d", f.port)
				if fixture.transport == "TCP" {
					occupied, err = net.Listen("tcp", address)
				} else {
					occupied, err = net.ListenPacket("udp", address)
				}
				if err != nil {
					t.Fatal("inactive Mieru retained its listener port", err)
				}
				_ = occupied.Close()
			}
			revoked := nativeInboundPeer(t, f, fixture.transport, 7, quicFixturePassword)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			conn, err := revoked.DialContext(ctx, "tcp", M.ParseSocksaddr(f.echo.Addr().String()))
			if err == nil {
				defer conn.Close()
				if nativeEcho(conn) == nil {
					t.Fatal("zero-user inbound admitted a fresh authenticated flow")
				}
			}
			for _, user := range []uint{7, 9} {
				if got := f.parents(t, user); got.Total != 0 {
					t.Fatal("revoked native user retained an admitted flow")
				}
			}
			replace(7, 9)
			reenabled := nativeInboundPeer(t, f, fixture.transport, 7, quicFixturePassword)
			_ = nativeStream(t, reenabled, f)
			nativeUDPWitness(t, reenabled)
		})
	}
}
