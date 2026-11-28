package runtime

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/sagernet/quic-go"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type diagnosticPacketPeerFixture struct {
	N.NetPacketConn
	peer M.Socksaddr
}

func (p diagnosticPacketPeerFixture) ReadPacket(b *buf.Buffer) (M.Socksaddr, error) {
	_, err := b.WriteString("reply")
	return p.peer, err
}

func TestDiagnosticDatagramChecksReplyEndpoint(t *testing.T) {
	target := M.ParseSocksaddr("192.0.2.1:3478")
	for _, peer := range []string{"192.0.2.2:3478", "192.0.2.1:3479", "target.invalid:3478"} {
		conn := diagnosticDatagramConn{packets: diagnosticPacketPeerFixture{peer: M.ParseSocksaddr(peer)}, target: target}
		var payload [16]byte
		if n, err := conn.Read(payload[:]); n != 0 || !errors.Is(err, errDiagnosticDatagramPeer) || payload != [16]byte{} {
			t.Errorf("unverified peer %s supplied a reply: %d bytes, %v", peer, n, err)
		}
	}
}

func TestDiagnosticVLESSDatagramUsesNativePacketPath(t *testing.T) {
	const fixtureID = "f0aa8764-88a4-4d8e-8ca8-24df4d750382"
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	server := NewCore()
	serverRaw := fmt.Appendf(nil, `{"log":{"disabled":true},"inbounds":[{"type":"vless","tag":"peer","listen":"127.0.0.1","listen_port":%d,"users":[{"name":"fixture","uuid":%q}],"multiplex":{"enabled":true}}],"outbounds":[{"type":"direct","tag":"peer-direct"}]}`, port, fixtureID)
	if err := server.Start(serverRaw); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Stop() })

	for _, tc := range []struct{ name, options string }{
		{"xudp", ""},
		{"fixed", `,"packet_encoding":""`},
		{"packetaddr", `,"packet_encoding":"packetaddr"`},
		{"h2mux", `,"multiplex":{"enabled":true,"protocol":"h2mux"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = peer.Close() })
			target := peer.LocalAddr().(*net.UDPAddr).AddrPort()
			client := NewCore()
			raw := fmt.Appendf(nil, `{"log":{"disabled":true},"outbounds":[{"type":"block","tag":"blocked"},{"type":"vless","tag":"selected","server":"127.0.0.1","server_port":%d,"uuid":%q,"network":"udp"%s}],"route":{"final":"blocked"}}`, port, fixtureID, tc.options)
			if err := client.Start(raw); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Stop() })
			choices, _ := client.DiagnosticChoices()
			if !slices.Equal(choices, []string{"selected"}) {
				t.Fatalf("unexpected diagnostic choices: %v", choices)
			}
			path, err := client.OpenOutboundDiagnostic("selected", "")
			if err != nil || !path.SupportsUDP() || path.TCPMode() != DiagnosticTCPUnavailable {
				t.Fatalf("incorrect diagnostic capability: %v", err)
			}
			if _, err := path.DialUDP(t.Context(), netip.AddrPortFrom(target.Addr(), 443), func(netip.Addr) bool { return true }); !errors.Is(err, ErrDiagnosticTarget) {
				t.Fatalf("public diagnostic admitted loopback: %v", err)
			}
			// Bypass only the public target policy for this in-process peer.
			// The real provider, destination dispatch and generation fence run.
			destination := M.Socksaddr{Addr: target.Addr(), Port: target.Port()}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			conn, err := client.dialOutbound(ctx, "selected", "udp", destination, &path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
			defer stop()
			if got := conn.RemoteAddr().String(); got != target.String() {
				t.Fatalf("remote addr is the carrier, not the target: %s", got)
			}
			deadline, _ := ctx.Deadline()
			// h2mux streams can close on cancellation, but may not have
			// native deadlines (which STUN requires for timed retransmit).
			if err := conn.SetDeadline(deadline); err != nil && !(tc.name == "h2mux" && errors.Is(err, os.ErrInvalid)) {
				t.Fatal(err)
			}
			if err := peer.SetDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			request := bytes.Repeat([]byte{0x36}, 1200)
			if n, err := conn.Write(request); n != len(request) || err != nil {
				t.Fatalf("write: %d bytes, %v", n, err)
			}
			var data [1200]byte
			n, from, err := peer.ReadFromUDP(data[:])
			if err != nil || !bytes.Equal(data[:n], request) {
				t.Fatalf("selected target got %d bytes, %v", n, err)
			}
			if _, err := peer.WriteToUDP(data[:n], from); err != nil {
				t.Fatal(err)
			}
			if n, err := conn.Read(data[:]); err != nil || !bytes.Equal(data[:n], request) {
				t.Fatalf("packet reply: %d bytes, %v", n, err)
			}
			if tc.name == "h2mux" || tc.name == "xudp" {
				checkDiagnosticVLESSQUIC(t, client, path)
			}
			readDone := make(chan error, 1)
			go func() {
				_, err := conn.Read(make([]byte, 1200))
				readDone <- err
			}()
			if err := client.Stop(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-readDone:
				if err == nil {
					t.Fatal("retired path read another datagram")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("read outlived the retired Core generation")
			}
			if err := client.Start(raw); err != nil {
				t.Fatal(err)
			}
			if _, err := client.dialOutbound(t.Context(), "selected", "udp", destination, &path); !errors.Is(err, ErrStaleGeneration) {
				t.Fatalf("old diagnostic used a new Core generation: %v", err)
			}
		})
	}
}

func checkDiagnosticVLESSQUIC(t *testing.T, client *Core, path OutboundDiagnostic) {
	t.Helper()
	const alpn = "blockcheck-fixture"
	certFixture := httptest.NewTLSServer(http.NotFoundHandler())
	defer certFixture.Close()
	server, err := quic.ListenAddr("127.0.0.1:0", &tls.Config{Certificates: certFixture.TLS.Certificates, NextProtos: []string{alpn}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	target := server.Addr().(*net.UDPAddr).AddrPort()
	dst := M.Socksaddr{Addr: target.Addr(), Port: target.Port()}
	packets, err := client.dialOutbound(ctx, "selected", "udp", dst, &path)
	if err != nil {
		t.Fatal(err)
	}
	defer packets.Close()
	stop := context.AfterFunc(ctx, func() { _ = packets.Close() })
	defer stop()
	roots := x509.NewCertPool()
	roots.AddCert(certFixture.Certificate())
	session, err := quic.DialConn(ctx, packets, &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", NextProtos: []string{alpn}}, &quic.Config{DisablePathMTUDiscovery: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.CloseWithError(0, ""); err != nil {
			t.Logf("closing the test QUIC session: %v", err)
		}
	}()
	if state := session.ConnectionState().TLS; len(state.VerifiedChains) == 0 || state.NegotiatedProtocol != alpn {
		t.Fatalf("QUIC did not authenticate the fixture target: %+v", state)
	}
	if _, err := server.Accept(ctx); err != nil {
		t.Fatalf("the selected QUIC target did not accept the session: %v", err)
	}
}
