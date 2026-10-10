//go:build with_quic

package runtime

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/inboundidentity"
	"github.com/MalenkiySolovey/solovey-ui/core/tracker"
	"github.com/sagernet/quic-go"
	"github.com/sagernet/quic-go/http3"
	"github.com/sagernet/quic-go/quicvarint"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-quic/hysteria"
	"github.com/sagernet/sing-quic/tuic"
)

type quicFixture struct {
	kind    string
	core    *Core
	inbound map[string]any
	roots   *x509.CertPool
	port    int
	echo    net.Listener
}

const quicFixturePassword = "synthetic-loopback-only"

func quicPort(t *testing.T) int {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func quicBindings(users ...uint) []inboundidentity.Binding {
	result := []inboundidentity.Binding{}
	for _, user := range users {
		result = append(result, inboundidentity.Binding{Inbound: "quic-fixture", Principal: fmt.Sprintf("verified-%d", user), ClientID: user})
	}
	return result
}

func (f *quicFixture) users(users ...uint) {
	list := []any{}
	for _, user := range users {
		entry := map[string]any{"name": fmt.Sprintf("verified-%d", user), "password": quicFixturePassword}
		if f.kind == "hysteria" {
			delete(entry, "password")
			entry["auth_str"] = quicFixturePassword + strconv.Itoa(int(user))
		}
		if f.kind == "hysteria2" {
			entry["password"] = quicFixturePassword + strconv.Itoa(int(user))
		}
		if f.kind == "tuic" {
			entry["uuid"] = fmt.Sprintf("00000000-0000-4000-8000-%012d", user)
		}
		list = append(list, entry)
	}
	f.inbound["users"] = list
}

func (f *quicFixture) raw() []byte {
	raw, _ := json.Marshal(map[string]any{"log": map[string]any{"disabled": true}, "inbounds": []any{f.inbound}, "outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}})
	return raw
}

func newQUICFixture(t *testing.T, kind string, observers ...tracker.IPObserver) *quicFixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"quic-fixture.invalid"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	public := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	private := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(public) {
		t.Fatal("fixture certificate")
	}
	f := &quicFixture{kind: kind, core: NewCore(observers...), roots: roots, port: quicPort(t)}
	alpn := "h3"
	if kind == "hysteria" {
		alpn = hysteria.DefaultALPN
	}
	f.inbound = map[string]any{"type": kind, "tag": "quic-fixture", "listen": "127.0.0.1", "listen_port": f.port,
		"tls": map[string]any{"enabled": true, "alpn": []string{alpn}, "certificate": []string{string(public)}, "key": []string{string(private)}}}
	if kind == "hysteria" {
		f.inbound["up_mbps"], f.inbound["down_mbps"] = 100, 100
	}
	f.users(7, 9)
	if err := f.core.StartWithBindings(f.raw(), quicBindings(7, 9)); err != nil {
		t.Fatal("QUIC fixture core start failed:", err)
	}
	t.Cleanup(func() {
		if err := f.core.Stop(); err != nil {
			t.Error("fixture stop:", err)
		}
	})
	f.echo, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	var access sync.Mutex
	var conns []net.Conn
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			conn, err := f.echo.Accept()
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
		_ = f.echo.Close()
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
			t.Error("echo listener/connection leak")
		}
	})
	if kind == "hysteria2" {
		inbound, _ := f.core.instance.Inbound().Get("quic-fixture")
		if _, preserved := inbound.(adapter.InterfaceUpdateListener); !preserved {
			t.Fatal("lost Hysteria2 optional interface")
		}
	}
	return f
}

type quicPeer struct {
	conn      *quic.Conn
	kind      string
	transport *http3.Transport
}

func (f *quicFixture) peer(t *testing.T, user uint) *quicPeer {
	t.Helper()
	peer, err := f.dial(t, user)
	if err != nil {
		t.Fatal("authenticated QUIC fixture failed:", err)
	}
	t.Cleanup(func() {
		if peer.transport != nil {
			_ = peer.transport.Close()
		}
		_ = peer.conn.CloseWithError(0, "fixture cleanup")
	})
	return peer
}

func (f *quicFixture) dial(t *testing.T, user uint) (*quicPeer, error) {
	return f.dialAt(t, user, net.JoinHostPort("127.0.0.1", strconv.Itoa(f.port)))
}

func (f *quicFixture) dialAt(t *testing.T, user uint, address string) (*quicPeer, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	alpn := "h3"
	if f.kind == "hysteria" {
		alpn = hysteria.DefaultALPN
	}
	config := &tls.Config{RootCAs: f.roots, ServerName: "quic-fixture.invalid", NextProtos: []string{alpn}, MinVersion: tls.VersionTLS13}
	peer := &quicPeer{kind: f.kind}
	if f.kind == "hysteria2" {
		peer.transport = &http3.Transport{TLSClientConfig: config, QUICConfig: &quic.Config{EnableDatagrams: true}, Dial: func(ctx context.Context, _ string, tc *tls.Config, qc *quic.Config) (*quic.Conn, error) {
			conn, err := quic.DialAddr(ctx, address, tc, qc)
			peer.conn = conn
			return conn, err
		}}
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://hysteria/auth", nil)
		req.Header.Set("Hysteria-Auth", quicFixturePassword+strconv.Itoa(int(user)))
		req.Header.Set("Hysteria-CC-RX", "0")
		response, err := peer.transport.RoundTrip(req)
		if err != nil {
			_ = peer.transport.Close()
			return nil, err
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != 233 {
			_ = peer.transport.Close()
			return nil, errors.New("Hysteria2 authentication rejected")
		}
	} else {
		conn, err := quic.DialAddr(ctx, address, config, &quic.Config{EnableDatagrams: true})
		if err != nil {
			return nil, err
		}
		peer.conn = conn
		if f.kind == "hysteria" {
			stream, err := conn.OpenStreamSync(ctx)
			if err != nil {
				_ = conn.CloseWithError(0, "")
				return nil, err
			}
			_ = stream.SetDeadline(time.Now().Add(3 * time.Second))
			err = hysteria.WriteClientHello(stream, hysteria.ClientHello{Auth: quicFixturePassword + strconv.Itoa(int(user)), SendBPS: 12500000, RecvBPS: 12500000})
			if err == nil {
				var response *hysteria.ServerHello
				response, err = hysteria.ReadServerHello(stream)
				if err == nil && !response.OK {
					err = errors.New("Hysteria authentication rejected")
				}
			}
			if err != nil {
				_ = conn.CloseWithError(0, "")
				return nil, err
			}
			_ = stream.SetDeadline(time.Time{})
		} else {
			stream, err := conn.OpenUniStreamSync(ctx)
			if err != nil {
				_ = conn.CloseWithError(0, "")
				return nil, err
			}
			var id [16]byte
			id[6], id[8], id[15] = 0x40, 0x80, byte(user)
			state := conn.ConnectionState()
			token, err := state.TLS.ExportKeyingMaterial(string(id[:]), []byte(quicFixturePassword), 32)
			if err != nil {
				_ = conn.CloseWithError(0, "")
				return nil, err
			}
			packet := append([]byte{tuic.Version, tuic.CommandAuthenticate}, id[:]...)
			packet = append(packet, token...)
			if _, err := stream.Write(packet); err != nil {
				_ = conn.CloseWithError(0, "")
				return nil, err
			}
			_ = stream.Close()
		}
	}
	// TUIC has no authentication ACK. A routed echo is the acceptance barrier.
	if err := peer.echo(t.Context(), f.echo.Addr().String()); err != nil {
		if peer.transport != nil {
			_ = peer.transport.Close()
		}
		_ = peer.conn.CloseWithError(0, "")
		return nil, err
	}
	return peer, nil
}

func (p *quicPeer) echo(ctx context.Context, address string) error {
	stream, err := p.echoStream(ctx, address)
	if err != nil {
		return err
	}
	stream.CancelRead(0)
	return stream.Close()
}

func (p *quicPeer) echoStream(ctx context.Context, address string) (*quic.Stream, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	stream, err := p.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			stream.CancelRead(0)
			_ = stream.Close()
		}
	}()
	_ = stream.SetDeadline(time.Now().Add(3 * time.Second))
	_, rawPort, _ := net.SplitHostPort(address)
	port, _ := strconv.ParseUint(rawPort, 10, 16)
	switch p.kind {
	case "hysteria":
		request := hysteria.WriteClientRequest(hysteria.ClientRequest{Host: "127.0.0.1", Port: uint16(port)}, nil)
		_, err = stream.Write(request.Bytes())
		request.Release()
		if err != nil {
			return nil, err
		}
		response, err := hysteria.ReadServerResponse(stream)
		if err != nil || !response.OK {
			return nil, errors.New("Hysteria routed request rejected")
		}
	case "hysteria2":
		// Official H2 proxy.go wire format, without randomized padding.
		request := quicvarint.Append(nil, 0x401)
		request = quicvarint.Append(request, uint64(len(address)))
		request = append(request, address...)
		request = quicvarint.Append(request, 0)
		if _, err := stream.Write(request); err != nil {
			return nil, err
		}
		var status [1]byte
		if _, err := io.ReadFull(stream, status[:]); err != nil {
			return nil, err
		}
		if status[0] != 0 {
			return nil, errors.New("Hysteria2 routed request rejected")
		}
		for i := 0; i < 2; i++ {
			length, err := quicvarint.Read(quicvarint.NewReader(stream))
			if err != nil || length > 4096 {
				return nil, errors.New("invalid fixture response")
			}
			if _, err := io.CopyN(io.Discard, stream, int64(length)); err != nil {
				return nil, err
			}
		}
	case "tuic":
		request := []byte{tuic.Version, tuic.CommandConnect, 1, 127, 0, 0, 1, 0, 0}
		binary.BigEndian.PutUint16(request[7:], uint16(port))
		if _, err := stream.Write(request); err != nil {
			return nil, err
		}
	}
	if _, err := stream.Write([]byte("ok")); err != nil {
		return nil, err
	}
	var reply [2]byte
	if _, err := io.ReadFull(stream, reply[:]); err != nil {
		return nil, err
	}
	if string(reply[:]) != "ok" {
		return nil, errors.New("authenticated echo mismatch")
	}
	success = true
	return stream, nil
}

func assertQUICRemoteClose(t *testing.T, peer *quicPeer) {
	t.Helper()
	select {
	case <-peer.conn.Context().Done():
		var app *quic.ApplicationError
		if !errors.As(context.Cause(peer.conn.Context()), &app) || !app.Remote {
			t.Fatal("intentional remote CONNECTION_CLOSE missing")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("authenticated parent remained open beyond loopback close bound")
	}
}

func (f *quicFixture) parents(t *testing.T, client uint) ConnectionSnapshot {
	t.Helper()
	snapshot, err := f.core.Connections(t.Context(), f.core.privateAPI.generation, client, 100)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), "verified-") || strings.Contains(string(encoded), quicFixturePassword) {
		t.Fatal("private authenticated principal leaked")
	}
	return snapshot
}

func (f *quicFixture) replace(users ...uint) error {
	f.port = quicPortForReplacement()
	f.inbound["listen_port"] = f.port
	f.users(users...)
	raw, _ := json.Marshal(f.inbound)
	return f.core.AddInboundWithBindings(raw, quicBindings(users...))
}

func quicPortForReplacement() int {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return 0
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	_ = conn.Close()
	return port
}

func TestQUICTUICEarlyListenerParentControl(t *testing.T) {
	f := newQUICFixture(t, "tuic")
	f.inbound["zero_rtt_handshake"] = true
	if err := f.replace(7, 9); err != nil {
		t.Fatal(err)
	}
	first, other := f.peer(t, 7), f.peer(t, 9)
	observed := f.parents(t, 7)
	if len(observed.Parents) != 1 {
		t.Fatal("early listener lost authenticated handle")
	}
	request := DisconnectRequest{Generation: observed.Generation, ClientID: 7, Parents: []QUICParentTarget{observed.Parents[0].QUICParentTarget}}
	if result, err := f.core.Disconnect(t.Context(), request); err != nil || result.ParentsClosed != 1 {
		t.Fatal("early listener kick failed", err)
	}
	assertQUICRemoteClose(t, first)
	if err := other.echo(t.Context(), f.echo.Addr().String()); err != nil {
		t.Fatal("early listener kicked another user", err)
	}
	reconnected := f.peer(t, 7)
	if err := f.core.RemoveInbound("quic-fixture"); err != nil {
		t.Fatal(err)
	}
	assertQUICRemoteClose(t, other)
	assertQUICRemoteClose(t, reconnected)
	t.Log("TUIC ListenEarly configuration, completed authenticated handshake: kick/reconnect/removal PASS; no 0-RTT resumption claim")
}

func TestQUICParentLifecycle(t *testing.T) {
	for _, kind := range []string{"hysteria", "hysteria2", "tuic"} {
		for _, action := range []string{"remove", "box-core-shutdown", "hot-replacement", "restart-reconnect"} {
			t.Run(kind+"/"+action, func(t *testing.T) {
				f := newQUICFixture(t, kind)
				peer := f.peer(t, 7)
				observed := f.parents(t, 7)
				if len(observed.Parents) != 1 || observed.Parents[0].ClientID != 7 || observed.Parents[0].Epoch == "" {
					t.Fatal("authenticated parent identity not proven")
				}
				switch action {
				case "remove":
					if err := f.core.RemoveInbound("quic-fixture"); err != nil {
						t.Fatal(err)
					}
				case "box-core-shutdown", "restart-reconnect":
					if err := f.core.Stop(); err != nil {
						t.Fatal(err)
					}
				case "hot-replacement":
					if err := f.replace(7, 9); err != nil {
						t.Fatal(err)
					}
				}
				assertQUICRemoteClose(t, peer)
				if action == "restart-reconnect" {
					if err := f.core.StartWithBindings(f.raw(), quicBindings(7, 9)); err != nil {
						t.Fatal(err)
					}
					reconnected := f.peer(t, 7)
					if err := reconnected.echo(t.Context(), f.echo.Addr().String()); err != nil {
						t.Fatal(err)
					}
					_, err := f.core.Disconnect(t.Context(), DisconnectRequest{Generation: observed.Generation, ClientID: 7, Parents: []QUICParentTarget{observed.Parents[0].QUICParentTarget}})
					if !errors.Is(err, ErrStaleGeneration) {
						t.Fatal("stale runtime generation accepted")
					}
				}
				if action == "hot-replacement" {
					newPeer := f.peer(t, 7)
					_, err := f.core.Disconnect(t.Context(), DisconnectRequest{Generation: observed.Generation, ClientID: 7, Parents: []QUICParentTarget{observed.Parents[0].QUICParentTarget}})
					if !errors.Is(err, ErrStaleInboundEpoch) {
						t.Fatal("stale inbound epoch accepted")
					}
					if err := newPeer.echo(t.Context(), f.echo.Addr().String()); err != nil {
						t.Fatal("new epoch parent was affected")
					}
				}
				if action == "remove" || action == "box-core-shutdown" {
					conn, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(f.port)))
					if err != nil {
						t.Fatal("UDP listener leaked")
					}
					_ = conn.Close()
				}
				t.Log("authenticated parent + remote CONNECTION_CLOSE + lifecycle assertion PASS; local integration only")
			})
		}
	}
}

func TestQUICAuthenticatedSelectiveKickAndReenable(t *testing.T) {
	for _, kind := range []string{"hysteria", "hysteria2", "tuic"} {
		t.Run(kind, func(t *testing.T) {
			f := newQUICFixture(t, kind)
			first := f.peer(t, 7)
			second := f.peer(t, 9)
			one, two := f.parents(t, 7), f.parents(t, 9)
			if len(one.Parents) != 1 || len(two.Parents) != 1 || one.Parents[0].ParentID == two.Parents[0].ParentID {
				t.Fatal("different authenticated handles not bound")
			}
			if first.conn.LocalAddr().(*net.UDPAddr).IP.String() != second.conn.LocalAddr().(*net.UDPAddr).IP.String() {
				t.Fatal("fixture did not share source IP")
			}
			mixed := DisconnectRequest{Generation: one.Generation, ClientID: 7, Parents: []QUICParentTarget{one.Parents[0].QUICParentTarget, two.Parents[0].QUICParentTarget}}
			if _, err := f.core.Disconnect(t.Context(), mixed); !errors.Is(err, ErrQUICParentIdentity) {
				t.Fatal("mixed foreign target accepted")
			}
			if err := first.echo(t.Context(), f.echo.Addr().String()); err != nil {
				t.Fatal("batch validation mutated before rejection")
			}
			request := DisconnectRequest{Generation: one.Generation, ClientID: 7, Parents: []QUICParentTarget{one.Parents[0].QUICParentTarget}}
			result, err := f.core.Disconnect(t.Context(), request)
			if err != nil || result.Outcome != "QUIC_PARENTS_CLOSED" || result.ParentsClosed != 1 || !result.ParentClosed {
				t.Fatalf("kick result=%+v err=%v", result, err)
			}
			assertQUICRemoteClose(t, first)
			if err := second.echo(t.Context(), f.echo.Addr().String()); err != nil {
				t.Fatal("same-IP other user disconnected")
			}
			reconnected := f.peer(t, 7)
			if result, err := f.core.Disconnect(t.Context(), request); err != nil || result.Outcome != "ALREADY_GONE" || result.ParentClosed {
				t.Fatal("old handle affected a reconnect")
			}
			if err := reconnected.echo(t.Context(), f.echo.Addr().String()); err != nil {
				t.Fatal("reconnect lockout")
			}
			if err := f.replace(9); err != nil {
				t.Fatal(err)
			}
			assertQUICRemoteClose(t, reconnected)
			assertQUICRemoteClose(t, second)
			if rejected, err := f.dial(t, 7); err == nil {
				_ = rejected.conn.CloseWithError(0, "")
				t.Fatal("disabled/removed credential still authenticated")
			}
			if snapshot := f.parents(t, 7); snapshot.Total != 0 || snapshot.ParentTotal != 0 {
				t.Fatal("rejected user admitted/accounted")
			}
			if err := f.replace(7, 9); err != nil {
				t.Fatal(err)
			}
			_ = f.peer(t, 7)
			_ = f.peer(t, 9)
			t.Log("same-IP authenticated selective kick, batch authorization, exact handle, reconnect, removal/disable and reenable PASS")
		})
	}
}

func TestQUICConcurrentCloseAndReplacement(t *testing.T) {
	for _, kind := range []string{"hysteria", "hysteria2", "tuic"} {
		t.Run(kind, func(t *testing.T) {
			f := newQUICFixture(t, kind)
			old := f.peer(t, 7)
			observed := f.parents(t, 7)
			start := make(chan struct{})
			replaced, kicked := make(chan error, 1), make(chan error, 1)
			go func() { <-start; replaced <- f.replace(7, 9) }()
			go func() {
				<-start
				_, err := f.core.Disconnect(t.Context(), DisconnectRequest{Generation: observed.Generation, ClientID: 7, Parents: []QUICParentTarget{observed.Parents[0].QUICParentTarget}})
				kicked <- err
			}()
			close(start)
			if err := <-replaced; err != nil {
				t.Fatal(err)
			}
			if err := <-kicked; err != nil && !errors.Is(err, ErrStaleInboundEpoch) {
				t.Fatal(err)
			}
			assertQUICRemoteClose(t, old)
			peer := f.peer(t, 9)
			if err := peer.echo(t.Context(), f.echo.Addr().String()); err != nil {
				t.Fatal("replacement parent affected")
			}
		})
	}
}
