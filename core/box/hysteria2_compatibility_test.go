//go:build with_quic

package box

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"

	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	entityprotocol "github.com/MalenkiySolovey/solovey-ui/internal/entities/protocol"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
)

// Target-core replay of the exact accepted old local peer proposition. No issuer or operator data.
func TestHysteria2LegacyMigrationPeers(t *testing.T) {

	for _, algorithm := range []string{"ECDSA", "RSA", "Ed25519"} {
		t.Run(algorithm, func(t *testing.T) {
			var private any
			var public any
			var err error
			switch algorithm {
			case "ECDSA":
				k, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				private, err = k, e
				if e == nil {
					public = &k.PublicKey
				}
			case "RSA":
				k, e := rsa.GenerateKey(rand.Reader, 2048)
				private, err = k, e
				if e == nil {
					public = &k.PublicKey
				}
			case "Ed25519":
				public, private, err = ed25519.GenerateKey(rand.Reader)
			}
			if err != nil {
				t.Fatal("fake key generation failed")
			}
			certDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "local-peer.invalid"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"local-peer.invalid"}}, &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "local-peer.invalid"}}, public, private)
			if err != nil {
				t.Fatal("fake certificate generation failed")
			}
			keyDER, err := x509.MarshalPKCS8PrivateKey(private)
			if err != nil {
				t.Fatal("fake key encoding failed")
			}
			certificate := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
			key := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
			reserve, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := reserve.LocalAddr().(*net.UDPAddr).Port
			reserve.Close()
			echo, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer echo.Close()
			go func() {
				for {
					c, e := echo.Accept()
					if e != nil {
						return
					}
					go func() { defer c.Close(); io.Copy(c, c) }()
				}
			}()
			packet, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer packet.Close()
			go func() {
				data := make([]byte, 4096)
				for {
					n, a, e := packet.ReadFrom(data)
					if e != nil {
						return
					}
					packet.WriteTo(data[:n], a)
				}
			}()
			raw, _ := json.Marshal(map[string]any{"log": map[string]any{"disabled": true}, "inbounds": []any{map[string]any{"type": "hysteria2", "tag": "peer", "listen": "127.0.0.1", "listen_port": port, "users": []any{map[string]any{"name": "fixture", "password": "fixture-noncredential"}}, "tls": map[string]any{"enabled": true, "certificate": []string{certificate}, "key": []string{key}}}}, "outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}, map[string]any{"type": "hysteria2", "tag": "client", "server": "127.0.0.1", "server_port": port, "password": "fixture-noncredential", "up_mbps": 10, "down_mbps": 20, "tls": map[string]any{"enabled": true, "insecure": true}}}, "route": map[string]any{"final": "direct"}})
			prepared, err := entityprotocol.PrepareConfigUpgrade(raw)
			if err != nil {
				t.Fatal(err)
			}
			raw = prepared.Candidate
			ctx := registry.Context(context.Background())
			var opt option.Options
			if err := opt.UnmarshalJSONContext(ctx, raw); err != nil {
				t.Fatal(err)
			}
			instance, err := NewBox(Options{Context: ctx, Options: opt})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if err := instance.Start(); err != nil {
				t.Fatal(err)
			}
			client, ok := instance.Outbound().Outbound("client")
			if !ok {
				t.Fatal("client absent")
			}
			deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			stream, err := client.DialContext(deadline, "tcp", M.ParseSocksaddr(echo.Addr().String()))
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			stream.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err = stream.Write([]byte("tcp-proof")); err != nil {
				t.Fatal(err)
			}
			reply := make([]byte, 9)
			if _, err = io.ReadFull(stream, reply); err != nil || string(reply) != "tcp-proof" {
				t.Fatal("TCP echo failed")
			}
			datagrams, err := client.ListenPacket(deadline, M.ParseSocksaddr(packet.LocalAddr().String()))
			if err != nil {
				t.Fatal(err)
			}
			defer datagrams.Close()
			datagrams.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err = datagrams.WriteTo([]byte("udp-proof"), M.ParseSocksaddr(packet.LocalAddr().String())); err != nil {
				t.Fatal(err)
			}
			buffer := make([]byte, 32)
			n, _, err := datagrams.ReadFrom(buffer)
			if err != nil || string(buffer[:n]) != "udp-proof" {
				t.Fatal("UDP echo failed")
			}
		})
	}
}
