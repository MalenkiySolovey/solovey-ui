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
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	anytls "github.com/anytls/sing-anytls"
	"github.com/anytls/sing-anytls/padding"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type anyTLSOutboundWitness struct {
	authenticated chan bool
}

func (h *anyTLSOutboundWitness) NewConnectionEx(ctx context.Context, conn net.Conn, _, destination M.Socksaddr, _ N.CloseHandlerFunc) {
	defer conn.Close()
	user, _ := auth.UserFromContext[string](ctx)
	h.authenticated <- user == "outbound-fixture" && destination == M.ParseSocksaddr("target.invalid:443")
	data := make([]byte, 4)
	if _, err := io.ReadFull(conn, data); err == nil {
		_, _ = conn.Write(data)
	}
}

// Observe only the public client metadata from the pinned v0.0.11 wire format
// (service.go and session/frame.go). Authentication bytes are skipped, never
// stored. This checks the official core's existing metadata adapter against
// the actual compiled dependency types without adding private-field access.
type anyTLSMetadataWitness struct {
	net.Conn
	skip, phase, used int
	header            [7]byte
	settings          []byte
	metadata          chan string
}

func (c *anyTLSMetadataWitness) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	for remaining := p[:n]; len(remaining) != 0 && c.phase < 4; {
		if c.skip > 0 {
			used := min(c.skip, len(remaining))
			c.skip -= used
			remaining = remaining[used:]
			continue
		}
		switch c.phase {
		case 0: // Two-byte authentication-padding length after the skipped hash.
			used := copy(c.header[c.used:2], remaining)
			c.used += used
			remaining = remaining[used:]
			if c.used == 2 {
				c.skip, c.phase, c.used = int(binary.BigEndian.Uint16(c.header[:2])), 1, 0
			}
		case 1: // Seven-byte frame header; command 4 contains client settings.
			used := copy(c.header[c.used:], remaining)
			c.used += used
			remaining = remaining[used:]
			if c.used == len(c.header) {
				length := int(binary.BigEndian.Uint16(c.header[5:]))
				c.used = 0
				if c.header[0] == 4 && length > 0 && length <= 1024 {
					c.settings = make([]byte, length)
					c.phase = 2
				} else {
					c.skip = length
				}
			}
		case 2:
			used := copy(c.settings[c.used:], remaining)
			c.used += used
			remaining = remaining[used:]
			if c.used == len(c.settings) {
				for _, line := range strings.Split(string(c.settings), "\n") {
					if value, ok := strings.CutPrefix(line, "client="); ok {
						c.metadata <- value
					}
				}
				c.settings = nil
				c.phase = 4
			}
		}
	}
	return n, err
}

func TestAnyTLSOfficialBoxOutboundMetadataAndAuthenticatedTraffic(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"anytls-fixture.invalid"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	public := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	listener, err := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	witness := &anyTLSOutboundWitness{authenticated: make(chan bool, 1)}
	server, err := anytls.NewService(anytls.ServiceConfig{PaddingScheme: padding.DefaultPaddingScheme, Users: []anytls.User{{Name: "outbound-fixture", Password: "synthetic-loopback-only"}}, Handler: witness, Logger: logger.NOP()})
	if err != nil {
		t.Fatal(err)
	}
	metadata := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_ = server.NewConnection(t.Context(), &anyTLSMetadataWitness{Conn: conn, skip: 32, metadata: metadata}, M.SocksaddrFromNet(conn.RemoteAddr()), nil)
	}()
	core := NewCore()
	raw, _ := json.Marshal(map[string]any{"log": map[string]bool{"disabled": true}, "outbounds": []any{map[string]any{"type": "anytls", "tag": "actual-official-outbound", "server": "127.0.0.1", "server_port": listener.Addr().(*net.TCPAddr).Port, "password": "synthetic-loopback-only", "client_metadata": "solovey-owner-fixture", "tls": map[string]any{"enabled": true, "server_name": "anytls-fixture.invalid", "certificate": []string{string(public)}}}}})
	t.Cleanup(func() {
		_ = core.Stop()
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("authenticated parent did not close")
		}
	})
	if err := core.Start(raw); err != nil {
		t.Fatal("official AnyTLS outbound start", err)
	}
	outbound, ok := core.instance.Outbound().Outbound("actual-official-outbound")
	if !ok {
		t.Fatal("official outbound missing")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := outbound.DialContext(ctx, "tcp", M.ParseSocksaddr("target.invalid:443"))
	if err != nil {
		t.Fatal("validated TLS authenticated outbound", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("echo")); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4)
	if _, err := io.ReadFull(conn, data); err != nil || string(data) != "echo" {
		t.Fatal("authenticated response mismatch", err)
	}
	select {
	case value := <-metadata:
		if value != "solovey-owner-fixture" {
			t.Fatal("official metadata adapter did not preserve configured value")
		}
	case <-ctx.Done():
		t.Fatal("client metadata not observed")
	}
	select {
	case authenticated := <-witness.authenticated:
		if !authenticated {
			t.Fatal("authenticated principal or destination changed")
		}
	case <-ctx.Done():
		t.Fatal("authenticated flow not observed")
	}
}
