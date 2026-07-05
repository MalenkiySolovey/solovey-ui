package tlsprobe

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestNormalizeTargetAcceptsHostPortAndDefaultPort(t *testing.T) {
	host, port, err := normalizeTarget("example.com:8443", "")
	if err != nil {
		t.Fatal(err)
	}
	if host != "example.com" || port != "8443" {
		t.Fatalf("target = %s:%s, want example.com:8443", host, port)
	}
	host, port, err = normalizeTarget("example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if host != "example.com" || port != "443" {
		t.Fatalf("target = %s:%s, want example.com:443", host, port)
	}
}

func TestProbeRejectsPrivateTargetsBeforeDial(t *testing.T) {
	_, err := Probe(context.Background(), ProbeConfig{Server: "127.0.0.1", Port: "443", Timeout: time.Second})
	if err == nil {
		t.Fatal("expected loopback target to be rejected")
	}
	if !strings.Contains(err.Error(), "not allowed") && !strings.Contains(err.Error(), "disallowed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCertificatePublicKeySHA256ReturnsBase64Pin(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pin := CertificatePublicKeySHA256(cert)
	if pin == "" || strings.Contains(pin, " ") {
		t.Fatalf("invalid pin: %q", pin)
	}
}
