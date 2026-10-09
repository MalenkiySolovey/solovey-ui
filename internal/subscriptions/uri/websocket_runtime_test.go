package uri

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2raywebsocket"
	mux "github.com/sagernet/sing-mux"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type websocketMuxDialer struct {
	transport adapter.V2RayClientTransport
	dials     atomic.Int32
}

func (d *websocketMuxDialer) DialContext(ctx context.Context, _ string, _ M.Socksaddr) (net.Conn, error) {
	d.dials.Add(1)
	return d.transport.DialContext(ctx)
}

func (*websocketMuxDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.New("fixture accepts only TCP streams")
}

type websocketMuxEcho struct {
	payload []byte
	done    chan error
}

func (h *websocketMuxEcho) NewConnectionEx(_ context.Context, conn net.Conn, _, _ M.Socksaddr, _ N.CloseHandlerFunc) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	data := make([]byte, len(h.payload))
	_, err := io.ReadFull(conn, data)
	if err == nil && !bytes.Equal(data, h.payload) {
		err = errors.New("multiplexed stream payload changed")
	}
	if err == nil {
		_, err = conn.Write(data)
	}
	h.done <- err
}

func (*websocketMuxEcho) NewPacketConnectionEx(_ context.Context, conn N.PacketConn, _, _ M.Socksaddr, _ N.CloseHandlerFunc) {
	_ = conn.Close()
}

type websocketMuxParent struct {
	service *mux.Service
	done    chan struct{}
	started atomic.Int32
}

func (h *websocketMuxParent) NewConnectionEx(ctx context.Context, conn net.Conn, source, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	h.started.Add(1)
	defer func() { _ = conn.Close(); h.done <- struct{}{} }()
	h.service.NewConnectionEx(ctx, conn, source, destination, onClose)
}

// The generated URI is decoded into the pinned official WebSocket transport.
// Two real sing-mux streams share that parent, including early handshake bytes
// and later frames. Query-byte preservation has a separate codec fixture:
// pinned sing's URLSetPath treats a query-looking path as a literal path.
func TestWave4WebSocketURIWorksWithPinnedRuntimeAndMux(t *testing.T) {
	for _, max := range []uint32{0, 1, 32, 8192} {
		t.Run(strconv.Itoa(int(max)), func(t *testing.T) {
			config := json.RawMessage(`{"vless":{"uuid":"11111111-1111-4111-8111-111111111111"}}`)
			inbound := wave4Inbound(t, "vless", "example.com", map[string]any{"transport": map[string]any{"type": "ws", "path": "/fixture", "max_early_data": max, "early_data_header_name": "Sec-WebSocket-Protocol"}}, nil)
			links, err := Generate(config, inbound, "")
			if err != nil {
				t.Fatal(err)
			}
			parsed, _, err := Parse(links[0], 0)
			if err != nil {
				t.Fatal(err)
			}
			var options option.V2RayWebsocketOptions
			if err := json.Unmarshal(wave4JSON(t, (*parsed)["transport"]), &options); err != nil {
				t.Fatal(err)
			}
			payload := bytes.Repeat([]byte{0, 1, 127, 255}, 64)
			echo := &websocketMuxEcho{payload: payload, done: make(chan error, 2)}
			logger := log.NewNOPFactory().Logger()
			service, err := mux.NewService(mux.ServiceOptions{Logger: logger, HandlerEx: echo, NewStreamContext: func(ctx context.Context, _ net.Conn) context.Context { return ctx }})
			if err != nil {
				t.Fatal(err)
			}
			parent := &websocketMuxParent{service: service, done: make(chan struct{}, 4)}
			server, err := v2raywebsocket.NewServer(t.Context(), logger, option.V2RayWebsocketOptions{Path: "/fixture", MaxEarlyData: max, EarlyDataHeaderName: "Sec-WebSocket-Protocol"}, nil, parent)
			if err != nil {
				t.Fatal(err)
			}
			type handshake struct{ query, early string }
			observed := make(chan handshake, 4)
			local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				observed <- handshake{r.URL.RawQuery, r.Header.Get("Sec-WebSocket-Protocol")}
				server.ServeHTTP(w, r)
			}))
			defer local.Close()
			transport, err := v2raywebsocket.NewClient(t.Context(), N.SystemDialer, M.ParseSocksaddr(local.Listener.Addr().String()), options, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer transport.Close()
			dialer := &websocketMuxDialer{transport: transport}
			client, err := mux.NewClient(mux.Options{Dialer: dialer, Logger: logger, Protocol: "smux", MaxConnections: 1, MaxStreams: 8})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				_ = client.Close()
				if parent.started.Load() > 0 {
					select {
					case <-parent.done:
					case <-time.After(3 * time.Second):
						t.Error("WebSocket parent did not close")
					}
				}
			}()
			for range 2 {
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				conn, err := client.DialContext(ctx, N.NetworkTCP, M.ParseSocksaddr("fixture.invalid:443"))
				cancel()
				if err != nil {
					t.Fatal(err)
				}
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				if _, err := conn.Write(payload); err != nil {
					_ = conn.Close()
					t.Fatal(err)
				}
				response := make([]byte, len(payload))
				_, err = io.ReadFull(conn, response)
				_ = conn.Close()
				if err != nil || !bytes.Equal(payload, response) {
					t.Fatal("multiplexed response did not round-trip")
				}
				select {
				case err := <-echo.done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("stream handler did not finish")
				}
			}
			if dialer.dials.Load() != 1 {
				t.Fatal("streams did not share one WebSocket parent")
			}
			select {
			case request := <-observed:
				if request.query != "" {
					t.Fatal("early-data URI extension leaked into the runtime request query")
				}
				early, err := base64.RawURLEncoding.DecodeString(request.early)
				if err != nil || (max == 0 && len(early) != 0) || (max > 0 && (len(early) == 0 || len(early) > int(max))) {
					t.Fatal("early data header does not match runtime policy")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("WebSocket upgrade was not observed")
			}
		})
	}
}
