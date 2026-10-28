//go:build shtorm_extended && with_quic

package runtime

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/sagernet/quic-go/http3"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/experimental/cachefile"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	native "github.com/sagernet/sing-box/protocol/masque"
	transport "github.com/sagernet/sing-box/transport/masque"
	M "github.com/sagernet/sing/common/metadata"
)

func TestNativeMASQUEL4RegisteredTrafficAndRestart(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := transport.GenerateCert(key, &key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http3.Server{
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{{Certificate: cert, PrivateKey: key}}, ClientAuth: tls.RequireAnyClientCert},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodConnect || r.Host != "192.0.2.1:443" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
			stream := w.(http3.HTTPStreamer).HTTPStream()
			defer stream.Close()
			_, _ = io.Copy(stream, stream)
		}),
	}
	serverDone := make(chan struct{})
	go func() { defer close(serverDone); _ = server.Serve(packet) }()
	t.Cleanup(func() { _ = server.Close(); _ = packet.Close(); <-serverDone })
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := json.Marshal(native.Config{PrivateKey: base64.StdEncoding.EncodeToString(private), EndpointPubKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})), IPv4: "192.0.2.2", IPv6: "2001:db8::2"})
	if err != nil {
		t.Fatal(err)
	}
	// Seed through the actual cache owner. Native bootstrap consumes this profile
	// instead of contacting a profile service or enrolling synthetic credentials.
	path := filepath.Join(t.TempDir(), "cache.db")
	logger := log.NewNOPFactory().Logger()
	scope := adapter.NewScope(context.Background(), logger)
	cache := cachefile.New(context.Background(), logger, option.CacheFileOptions{Path: path, StoreMASQUEConfig: true})
	if err := cache.Start(adapter.StartStateInitialize, scope); err != nil {
		t.Fatal(err)
	}
	if err := cache.SaveMASQUEConfig("native-masque", &adapter.SavedBinary{Content: profile, LastUpdated: time.Now()}); err != nil {
		_ = scope.Close()
		t.Fatal(err)
	}
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"log":          map[string]any{"disabled": true},
		"experimental": map[string]any{"cache_file": map[string]any{"enabled": true, "path": path, "store_masque_config": true}},
		"outbounds":    []any{map[string]any{"type": "masque", "tag": "native-masque", "use_l4_proxy": true, "address": "127.0.0.1", "port": packet.LocalAddr().(*net.UDPAddr).Port, "handshake_timeout": "3s", "tls": map[string]any{"server_name": "localhost"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := NewCore()
	t.Cleanup(func() {
		if err := client.Stop(); err != nil {
			t.Error(err)
		}
	})
	for generation := 0; generation < 2; generation++ {
		if err := client.Start(raw); err != nil {
			t.Fatal("native MASQUE start", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		stream, err := client.DialOutbound(ctx, "native-masque", "tcp", M.ParseSocksaddr("192.0.2.1:443"))
		cancel()
		if err != nil {
			t.Fatal("registered MASQUE L4 dial", err)
		}
		if err := nativeEcho(stream); err != nil {
			_ = stream.Close()
			t.Fatal("native MASQUE traffic", err)
		}
		if err := client.Stop(); err != nil {
			t.Fatal("native MASQUE stop", err)
		}
		if nativeEcho(stream) == nil {
			t.Fatal("retired MASQUE runtime kept forwarding")
		}
		_ = stream.Close()
	}
}
