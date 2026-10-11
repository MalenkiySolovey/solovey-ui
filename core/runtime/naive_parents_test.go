//go:build with_quic

package runtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/sagernet/quic-go"
	"github.com/sagernet/quic-go/http3"
)

type naivePeer struct {
	*quicPeer
	client *http3.ClientConn
}

func (f *quicFixture) naivePeer(t *testing.T) *naivePeer {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	conn, err := quic.DialAddr(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(f.port)), &tls.Config{
		RootCAs: f.roots, ServerName: "quic-fixture.invalid", NextProtos: []string{http3.NextProtoH3}, MinVersion: tls.VersionTLS13,
	}, &quic.Config{})
	if err != nil {
		t.Fatal("independent HTTP3 TLS connection failed", err)
	}
	transport := &http3.Transport{}
	peer := &naivePeer{quicPeer: &quicPeer{conn: conn, kind: "naive", transport: transport}, client: transport.NewClientConn(conn)}
	t.Cleanup(func() { _ = conn.CloseWithError(0, "fixture cleanup"); _ = transport.Close() })
	return peer
}

// Keep the accepted CONNECT stream open after its nonce barrier. The fixture
// can then distinguish a routed flow close from this exact parent's closure.
func (p *naivePeer) nonce(t *testing.T, address string, user uint) {
	t.Helper()
	if !p.nonceResult(t, address, user) {
		t.Fatal("authenticated HTTP3 routed nonce failed")
	}
}

func (p *naivePeer) nonceResult(t *testing.T, address string, user uint) bool {
	t.Helper()
	reader, writer := io.Pipe()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodConnect, "https://"+address, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = address
	req.Header.Set("Padding", "00000000000000000000000000000000")
	req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("verified-%d:%s", user, quicFixturePassword))))
	response, err := p.client.RoundTrip(req)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return false
	}
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close(); _ = response.Body.Close() })
	if response.StatusCode != 200 || response.ProtoMajor != 3 || p.conn.ConnectionState().TLS.NegotiatedProtocol != "h3" {
		return false
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	done := make(chan bool, 1)
	go func() {
		if _, err := writer.Write(append([]byte{0, 32, 0}, nonce...)); err != nil {
			done <- false
			return
		}
		header := make([]byte, 3)
		if _, err := io.ReadFull(response.Body, header); err != nil || binary.BigEndian.Uint16(header[:2]) != 32 {
			done <- false
			return
		}
		payload := make([]byte, 32+int(header[2]))
		_, err := io.ReadFull(response.Body, payload)
		done <- err == nil && bytes.Equal(payload[:32], nonce)
	}()
	select {
	case ok := <-done:
		return ok
	case <-time.After(3 * time.Second):
		t.Fatal("authenticated HTTP3 nonce deadline")
	}
	return false
}

func TestNaiveHTTP3ParentLifecycle(t *testing.T) {
	for _, action := range []string{"remove", "box-core-shutdown", "hot-replacement", "restart-reconnect"} {
		t.Run(action, func(t *testing.T) {
			f := newQUICFixture(t, "naive")
			peer := f.naivePeer(t)
			peer.nonce(t, f.echo.Addr().String(), 7)
			switch action {
			case "remove":
				if err := f.core.RemoveInbound("quic-fixture"); err != nil {
					t.Fatal(err)
				}
			case "box-core-shutdown", "restart-reconnect":
				if err := f.core.Stop(); err != nil {
					t.Fatal(err)
				}
				if err := f.core.Stop(); err != nil {
					t.Fatal("repeated stop", err)
				}
			case "hot-replacement":
				// Reuse the exact UDP port; removal must drain its old handles before
				// the same tag accepts a new inbound epoch on this endpoint.
				f.inbound["quic_congestion_control"] = "reno"
				raw, _ := json.Marshal(f.inbound)
				if err := f.core.RemoveInbound("quic-fixture"); err != nil {
					t.Fatal(err)
				}
				if err := f.core.AddInboundWithBindings(raw, quicBindings(7, 9)); err != nil {
					t.Fatal(err)
				}
			}
			assertQUICRemoteClose(t, peer.quicPeer)
			if action == "restart-reconnect" {
				if err := f.core.StartWithBindings(f.raw(), quicBindings(7, 9)); err != nil {
					t.Fatal(err)
				}
			}
			if action == "hot-replacement" || action == "restart-reconnect" {
				fresh := f.naivePeer(t)
				fresh.nonce(t, f.echo.Addr().String(), 7)
				if fresh.conn.Context().Err() != nil {
					t.Fatal("new parent closed by old owner")
				}
			} else {
				probe, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(f.port)))
				if err != nil {
					t.Fatal("UDP listener leaked", err)
				}
				_ = probe.Close()
			}
		})
	}
}

func TestNaiveHTTP3PerRequestPrincipalRemainsFlowOnly(t *testing.T) {
	f := newQUICFixture(t, "naive")
	peer := f.naivePeer(t)
	peer.nonce(t, f.echo.Addr().String(), 7)
	peer.nonce(t, f.echo.Addr().String(), 9)
	first, other := f.parents(t, 7), f.parents(t, 9)
	if len(first.Parents) != 0 || len(other.Parents) != 0 || len(first.Connections) != 1 || len(other.Connections) != 1 {
		t.Fatal("per-request authentication must not become a selective parent capability")
	}
	result, err := f.core.Disconnect(t.Context(), DisconnectRequest{Generation: first.Generation, ClientID: 7, FlowID: first.Connections[0].ID})
	if err != nil || result.Closed != 1 || result.ParentClosed || result.ParentControl != "not_supported" {
		t.Fatal("flow-only Naive disconnect contract changed", err)
	}
	if peer.conn.Context().Err() != nil {
		t.Fatal("one user's flow closed the shared parent")
	}
	peer.nonce(t, f.echo.Addr().String(), 9)
	peer.nonce(t, f.echo.Addr().String(), 7)
}

func TestNaiveHTTP3FailedStartupKeepsUDPReusable(t *testing.T) {
	f := newQUICFixture(t, "naive")
	if err := f.core.Stop(); err != nil {
		t.Fatal(err)
	}
	f.inbound["quic_congestion_control"] = "invalid-fixture-control"
	if err := f.core.StartWithBindings(f.raw(), quicBindings(7, 9)); err == nil {
		t.Fatal("invalid congestion accepted")
	}
	if err := f.core.Stop(); err != nil {
		t.Fatal("failed-start cleanup", err)
	}
	probe, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(f.port)))
	if err != nil {
		t.Fatal("failed startup leaked UDP", err)
	}
	_ = probe.Close()
	delete(f.inbound, "quic_congestion_control")
	if err := f.core.StartWithBindings(f.raw(), quicBindings(7, 9)); err != nil {
		t.Fatal("valid restart after failed startup", err)
	}
	f.naivePeer(t).nonce(t, f.echo.Addr().String(), 7)
}

func TestNaiveHTTP3RetiredEpochRejectsStreamsBeforeAccounting(t *testing.T) {
	f := newQUICFixture(t, "naive")
	peer := f.naivePeer(t)
	peer.nonce(t, f.echo.Addr().String(), 7)
	_ = f.core.instance.StatsTracker().GetStats()
	// CoreRuntime removal revokes this existing authenticated identity owner
	// before transport closure. It fences late requests without another parent
	// authentication/admission authority in the HTTP/3 lifecycle composition.
	f.core.instance.InboundIdentity().Revoke("quic-fixture")
	if peer.conn.Context().Err() != nil {
		t.Fatal("epoch-only fixture unexpectedly closed the parent")
	}
	if peer.nonceResult(t, f.echo.Addr().String(), 7) {
		t.Fatal("retired epoch routed another request on its old parent")
	}
	for _, stat := range f.core.instance.StatsTracker().GetStats() {
		if stat.Resource == "user" && stat.Tag == "verified-7" && stat.Traffic != 0 {
			t.Fatal("retired request was accounted")
		}
	}
}
