package ipcert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func makeSelfSignedCertPEM(t *testing.T, notAfter time.Time) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "203.0.113.7"},
		NotBefore:    notAfter.Add(-160 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func TestParseCertNotAfter(t *testing.T) {
	want := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	certPEM, _ := makeSelfSignedCertPEM(t, want)
	got, err := ParseCertNotAfter(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Fatalf("ParseCertNotAfter = %v, want %v", got, want)
	}

	if _, err := ParseCertNotAfter([]byte("not a pem")); err == nil {
		t.Fatal("ParseCertNotAfter on garbage = nil, want error")
	}

	certPEM2, keyPEM := makeSelfSignedCertPEM(t, want)
	withLeadingKey := append(append([]byte{}, keyPEM...), certPEM2...)
	got, err = ParseCertNotAfter(withLeadingKey)
	if err != nil {
		t.Fatalf("ParseCertNotAfter with leading key block: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("ParseCertNotAfter with leading key = %v, want %v", got, want)
	}

	leafWant := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	issuerWant := time.Date(2027, 1, 1, 9, 0, 0, 0, time.UTC)
	leafPEM, _ := makeSelfSignedCertPEM(t, leafWant)
	issuerPEM, _ := makeSelfSignedCertPEM(t, issuerWant)
	bundle := append(append([]byte{}, leafPEM...), issuerPEM...)
	got, err = ParseCertNotAfter(bundle)
	if err != nil {
		t.Fatalf("ParseCertNotAfter on bundle: %v", err)
	}
	if !got.Equal(leafWant) {
		t.Fatalf("ParseCertNotAfter bundle = %v, want leaf %v", got, leafWant)
	}

	if _, err := ParseCertNotAfter(keyPEM); err == nil {
		t.Fatal("ParseCertNotAfter on key-only PEM = nil, want error")
	}
}

func TestWriteCertFiles(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	certPEM, keyPEM := makeSelfSignedCertPEM(t, time.Now().Add(160*time.Hour))
	certPath, keyPath, err := WriteCertFiles("2001:db8::1", certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(certPath) != "ip-2001_db8__1.crt" {
		t.Fatalf("unexpected cert filename: %s", filepath.Base(certPath))
	}
	gotCert, err := os.ReadFile(certPath)
	if err != nil || string(gotCert) != string(certPEM) {
		t.Fatalf("cert file content mismatch: err=%v", err)
	}
	gotKey, err := os.ReadFile(keyPath)
	if err != nil || string(gotKey) != string(keyPEM) {
		t.Fatalf("key file content mismatch: err=%v", err)
	}
	if runtime.GOOS != "windows" {
		certInfo, err := os.Stat(certPath)
		if err != nil {
			t.Fatal(err)
		}
		if certInfo.Mode().Perm() != 0o600 {
			t.Fatalf("cert perm = %o, want 600", certInfo.Mode().Perm())
		}
		keyInfo, err := os.Stat(keyPath)
		if err != nil {
			t.Fatal(err)
		}
		if keyInfo.Mode().Perm() != 0o600 {
			t.Fatalf("key perm = %o, want 600", keyInfo.Mode().Perm())
		}
	}
	if _, _, err := WriteCertFiles("1.2.3.4", nil, keyPEM); err == nil {
		t.Fatal("WriteCertFiles with empty cert = nil, want error")
	}
}
