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
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
)

func TestMKCPNativeTLSTrafficAndRetirement(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"mkcp-fixture.invalid"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
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
	port := quicPort(t)
	transport := map[string]any{"type": "mkcp", "tti": 10, "mtu": 1350, "header_type": "wechat-video", "seed": "synthetic-native-mkcp-seed"}
	serverRaw, _ := json.Marshal(map[string]any{"log": map[string]bool{"disabled": true}, "inbounds": []any{map[string]any{"type": "vless", "tag": "mkcp-in", "listen": "127.0.0.1", "listen_port": port,
		"users": []any{map[string]any{"name": "verified", "uuid": "f0aa8764-88a4-4d8e-8ca8-24df4d750382"}}, "transport": transport,
		"tls": map[string]any{"enabled": true, "certificate": []string{public}, "key": []string{private}}}}, "outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}})
	outbound := map[string]any{"type": "vless", "tag": "mkcp-out", "server": "127.0.0.1", "server_port": port, "uuid": "f0aa8764-88a4-4d8e-8ca8-24df4d750382", "transport": transport,
		"tls": map[string]any{"enabled": true, "server_name": "mkcp-fixture.invalid", "certificate": []string{public}}}
	clientRaw, _ := json.Marshal(map[string]any{"log": map[string]bool{"disabled": true}, "outbounds": []any{outbound}})
	outboundRaw, _ := json.Marshal(outbound)
	server, client := NewCore(), NewCore()
	t.Cleanup(func() { _ = client.Stop(); _ = server.Stop() })
	echo := nativeTCPWitness(t)
	for range 2 {
		if err := server.Start(serverRaw); err != nil {
			t.Fatal("native mKCP listener start", err)
		}
		if err := client.Start(clientRaw); err != nil {
			t.Fatal("native mKCP client start", err)
		}
		for replacement := range 2 {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			stream, err := client.DialOutbound(ctx, "mkcp-out", "tcp", M.ParseSocksaddr(echo.Addr().String()))
			cancel()
			if err != nil {
				t.Fatal("native mKCP TLS dial", err)
			}
			if err := nativeEcho(stream); err != nil {
				_ = stream.Close()
				t.Fatal("native authenticated mKCP traffic", err)
			}
			if replacement == 0 {
				if err := client.RemoveOutbound("mkcp-out"); err != nil {
					t.Fatal("mKCP parent removal", err)
				}
			} else if err := client.Stop(); err != nil {
				t.Fatal("mKCP generation retirement", err)
			}
			if nativeEcho(stream) == nil {
				_ = stream.Close()
				t.Fatal("retired mKCP parent kept forwarding")
			}
			_ = stream.Close()
			if replacement == 0 {
				if err := client.AddOutbound(outboundRaw); err != nil {
					t.Fatal("mKCP parent recreation", err)
				}
			}
		}
		if err := server.Stop(); err != nil {
			t.Fatal("mKCP listener retirement", err)
		}
	}
}
