package entitytls

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	boxtls "github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	sbjson "github.com/sagernet/sing/common/json"
)

func TestMTLSSideCredentialsPinnedHandshakeAndTimeout(t *testing.T) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))
	makePair := func(serial int64, usage x509.ExtKeyUsage) (string, string, *x509.Certificate) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		leaf := &x509.Certificate{SerialNumber: big.NewInt(serial), DNSNames: []string{"peer.fixture.invalid"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter}
		der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		private, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), parsed
	}
	serverPEM, serverKey, serverCert := makePair(2, x509.ExtKeyUsageServerAuth)
	clientPEM, clientKey, _ := makePair(3, x509.ExtKeyUsageClientAuth)
	spki := sha256.Sum256(serverCert.RawSubjectPublicKeyInfo)
	pin := base64.StdEncoding.EncodeToString(spki[:])
	for _, mode := range []string{"text", "path", "missing-client", "wrong-pin"} {
		t.Run(mode, func(t *testing.T) {
			server := map[string]any{"enabled": true, "certificate": []string{serverPEM}, "key": []string{serverKey}, "client_authentication": "require-and-verify", "client_certificate": []string{caPEM}, "handshake_timeout": "2s", "max_version": "1.2"}
			client := map[string]any{"enabled": true, "server_name": "peer.fixture.invalid", "certificate": []string{caPEM}, "client_certificate": []string{clientPEM}, "client_key": []string{clientKey}, "certificate_public_key_sha256": []string{pin}, "insecure": false, "handshake_timeout": "2s", "max_version": "1.2"}
			if mode == "text" || mode == "wrong-pin" {
				delete(client, "certificate")
			} else {
				delete(client, "certificate_public_key_sha256")
			}
			if mode == "path" {
				root := t.TempDir()
				paths := map[string]string{}
				for key, value := range map[string]string{"ca": caPEM, "client-cert": clientPEM, "client-key": clientKey} {
					name := filepath.Join(root, key+".pem")
					if err := os.WriteFile(name, []byte(value), 0600); err != nil {
						t.Fatal(err)
					}
					paths[key] = name
				}
				delete(server, "client_certificate")
				server["client_certificate_path"] = []string{paths["ca"]}
				delete(client, "client_certificate")
				delete(client, "client_key")
				client["client_certificate_path"] = paths["client-cert"]
				client["client_key_path"] = paths["client-key"]
			}
			if mode == "missing-client" {
				delete(client, "client_certificate")
				delete(client, "client_key")
			}
			if mode == "wrong-pin" {
				client["certificate_public_key_sha256"] = []string{base64.StdEncoding.EncodeToString(make([]byte, 32))}
			}
			sRaw, _ := json.Marshal(server)
			cRaw, _ := json.Marshal(client)
			if len(TLSOptionsFindings("server", "server", sRaw)) != 0 || len(TLSOptionsFindings("client", "client", cRaw)) != 0 {
				t.Fatal("owner schema rejected valid side shapes")
			}
			var sOptions option.InboundTLSOptions
			var cOptions option.OutboundTLSOptions
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := sbjson.UnmarshalContext(ctx, sRaw, &sOptions); err != nil {
				t.Fatal(err)
			}
			if err := sbjson.UnmarshalContext(ctx, cRaw, &cOptions); err != nil {
				t.Fatal(err)
			}
			logger := log.NewNOPFactory().NewLogger("fixture")
			sConfig, err := boxtls.NewServer(ctx, logger, sOptions)
			if err != nil {
				t.Fatal("server TLS construction failed")
			}
			defer sConfig.Close()
			cConfig, err := boxtls.NewClient(ctx, logger, "peer.fixture.invalid", cOptions)
			if err != nil {
				t.Fatal("client TLS construction failed")
			}
			if sConfig.HandshakeTimeout() != 2*time.Second || cConfig.HandshakeTimeout() != 2*time.Second {
				t.Fatal("handshake timeout was not propagated")
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			serverDone := make(chan error, 1)
			go func() {
				connection, err := listener.Accept()
				if err != nil {
					serverDone <- err
					return
				}
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
				secure, err := boxtls.ServerHandshake(ctx, connection, sConfig)
				if err == nil {
					data := make([]byte, 1)
					_, err = io.ReadFull(secure, data)
					if err == nil {
						_, err = secure.Write(data)
					}
				}
				serverDone <- err
			}()
			connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
			secure, clientErr := boxtls.ClientHandshake(ctx, connection, cConfig)
			if clientErr == nil {
				_, clientErr = secure.Write([]byte{1})
				if clientErr == nil {
					data := make([]byte, 1)
					_, clientErr = io.ReadFull(secure, data)
				}
			}
			_ = connection.Close()
			serverErr := <-serverDone
			if mode == "missing-client" || mode == "wrong-pin" {
				if serverErr == nil || clientErr == nil {
					t.Fatal("invalid mutual authentication or pin was accepted")
				}
			} else if serverErr != nil || clientErr != nil {
				t.Fatal("local mutual TLS replay failed")
			}
		})
	}
}
