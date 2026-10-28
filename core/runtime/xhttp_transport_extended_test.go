//go:build shtorm_extended && with_quic

package runtime

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
)

// Both ends use the production provider registry. The split download leg
// authenticates with a client certificate, without a replacement HTTP server.
func TestXHTTPNativeTrafficAndRestart(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"xhttp-fixture.invalid"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	public := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	private := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	for _, mode := range []string{"packet-up", "stream-up", "stream-one", "split-download"} {
		t.Run(mode, func(t *testing.T) {
			probe, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := probe.Addr().(*net.TCPAddr).Port
			_ = probe.Close()
			server, client := NewCore(), NewCore()
			t.Cleanup(func() { _ = client.Stop(); _ = server.Stop() })
			echo := nativeTCPWitness(t)
			serverTLS := map[string]any{"enabled": true, "alpn": []string{"h2"}, "certificate": []string{public}, "key": []string{private}}
			clientTLS := map[string]any{"enabled": true, "alpn": []string{"h2"}, "server_name": "xhttp-fixture.invalid", "certificate": []string{public}}
			transport := map[string]any{"type": "xhttp", "mode": mode, "path": "/owned", "x_padding_bytes": "100-1000"}
			if mode == "split-download" {
				transport["mode"] = "packet-up"
				serverTLS["client_certificate"] = []string{public}
				clientTLS["client_certificate"], clientTLS["client_key"] = []string{public}, []string{private}
				transport["download"] = map[string]any{"server": "127.0.0.1", "server_port": port, "detour": "download-owner", "path": "/owned", "x_padding_bytes": "100-1000", "tls": clientTLS}
			}
			serverRaw, _ := json.Marshal(map[string]any{"log": map[string]bool{"disabled": true}, "inbounds": []any{map[string]any{"type": "vless", "tag": "xhttp-in", "listen": "127.0.0.1", "listen_port": port,
				"users": []any{map[string]any{"name": "verified", "uuid": "f0aa8764-88a4-4d8e-8ca8-24df4d750382"}}, "tls": serverTLS, "transport": map[string]any{"type": "xhttp", "mode": "auto", "path": "/owned", "x_padding_bytes": "100-1000"}}},
				"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}})
			clientRaw, _ := json.Marshal(map[string]any{"log": map[string]bool{"disabled": true}, "outbounds": []any{
				map[string]any{"type": "vless", "tag": "xhttp-out", "server": "127.0.0.1", "server_port": port, "uuid": "f0aa8764-88a4-4d8e-8ca8-24df4d750382", "tls": clientTLS, "transport": transport},
				map[string]any{"type": "direct", "tag": "download-owner"}}})
			for generation := 0; generation < 2; generation++ {
				if err := server.Start(serverRaw); err != nil {
					t.Fatal("native XHTTP server start", err)
				}
				if err := client.Start(clientRaw); err != nil {
					t.Fatal("native XHTTP client start", err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				stream, err := client.DialOutbound(ctx, "xhttp-out", "tcp", M.ParseSocksaddr(echo.Addr().String()))
				cancel()
				if err != nil {
					t.Fatal("native XHTTP dial", err)
				}
				if err := nativeEcho(stream); err != nil {
					_ = stream.Close()
					t.Fatal("native XHTTP traffic", err)
				}
				if err := client.Stop(); err != nil {
					t.Fatal("native XHTTP client stop", err)
				}
				if nativeEcho(stream) == nil {
					t.Fatal("retired XHTTP generation kept forwarding")
				}
				_ = stream.Close()
				if err := server.Stop(); err != nil {
					t.Fatal("native XHTTP listener teardown", err)
				}
			}
		})
	}
}
