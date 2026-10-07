//go:build with_utls

package coreinboundcontrol

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"strconv"
	"testing"
	"time"

	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/sagernet/sing-box/option"
)

// A real local TLS transaction exercises the exact target core/uTLS fallback
// envelope. All keys are ephemeral synthetic fixtures, never diagnostics.
func TestPinnedRuntimeNaturalTLSFallback(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"fixture.invalid"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}, &x509.Certificate{SerialNumber: big.NewInt(1)}, public, private)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []string{"trojan", "vless"} {
		t.Run(protocol, func(t *testing.T) {
			var sink net.Listener
			var err error
			if protocol == "vless" {
				sink, err = tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}, NextProtos: []string{"http/1.1"}})
			} else {
				sink, err = net.Listen("tcp", "127.0.0.1:0")
			}
			if err != nil {
				t.Fatal(err)
			}
			defer sink.Close()
			_, sinkPortString, _ := net.SplitHostPort(sink.Addr().String())
			sinkPort, _ := strconv.Atoi(sinkPortString)
			done := make(chan error, 1)
			payload := []byte("GET /synthetic-fallback HTTP/1.1\r\nHost: fixture.invalid\r\n\r\n")
			go func() {
				conn, err := sink.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				got := make([]byte, len(payload))
				_, err = io.ReadFull(conn, got)
				if err == nil && string(got) != string(payload) {
					err = io.ErrUnexpectedEOF
				}
				if err == nil {
					_, err = conn.Write([]byte("fallback-confirmed"))
				}
				done <- err
			}()
			reservation, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			_, portString, _ := net.SplitHostPort(reservation.Addr().String())
			port, _ := strconv.Atoi(portString)
			_ = reservation.Close()
			tlsOptions := map[string]any{"enabled": true, "certificate": []string{string(certPEM)}, "key": []string{string(keyPEM)}, "alpn": []string{"http/1.1"}}
			inbound := map[string]any{"type": protocol, "tag": "fallback-fixture", "listen": "127.0.0.1", "listen_port": port, "tls": tlsOptions}
			if protocol == "trojan" {
				inbound["fallback"] = map[string]any{"server": "127.0.0.1", "server_port": sinkPort}
			} else {
				realityKey, err := ecdh.X25519().GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				tlsOptions = map[string]any{"enabled": true, "server_name": "fixture.invalid", "reality": map[string]any{"enabled": true, "private_key": base64.RawURLEncoding.EncodeToString(realityKey.Bytes()), "short_id": []string{"0123456789abcdef"}, "handshake": map[string]any{"server": "127.0.0.1", "server_port": sinkPort}}}
				inbound["tls"] = tlsOptions
			}
			data, _ := json.Marshal(map[string]any{"log": map[string]bool{"disabled": true}, "inbounds": []any{inbound}, "outbounds": []any{map[string]string{"type": "direct", "tag": "direct"}}})
			ctx := registry.Context(context.Background())
			var options option.Options
			if err := options.UnmarshalJSONContext(ctx, data); err != nil {
				t.Fatal("fixture option decoding failed")
			}
			box, err := corebox.NewBox(corebox.Options{Context: ctx, Options: options})
			if err != nil {
				t.Fatal(err)
			}
			defer box.Close()
			if err := box.Start(); err != nil {
				t.Fatal(err)
			}
			conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", net.JoinHostPort("127.0.0.1", portString), &tls.Config{InsecureSkipVerify: true, ServerName: "fixture.invalid", NextProtos: []string{"http/1.1"}})
			if err != nil {
				t.Fatal("natural fallback TLS handshake failed", err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			if _, err := conn.Write(payload); err != nil {
				t.Fatal(err)
			}
			response := make([]byte, len("fallback-confirmed"))
			if _, err := io.ReadFull(conn, response); err != nil || string(response) != "fallback-confirmed" {
				t.Fatal("target fallback did not deliver exact sink response", err)
			}
			if err := <-done; err != nil {
				t.Fatal("fallback sink transaction failed", err)
			}
		})
	}
}
