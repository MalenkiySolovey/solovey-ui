package entitytls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestApplySelfSignedPublicKeyPin(t *testing.T) {
	certPEM, expectedPin := testSelfSignedCert(t)
	tls := model.Tls{
		Server: json.RawMessage(`{"enabled":true,"certificate":` + mustJSON(t, []string{certPEM}) + `}`),
		Client: json.RawMessage(`{"certificate":["old"],"certificate_path":"/old.pem"}`),
	}

	if !ApplySelfSignedPublicKeyPin(&tls) {
		t.Fatal("expected self-signed certificate to update client pin")
	}
	var client map[string]interface{}
	if err := json.Unmarshal(tls.Client, &client); err != nil {
		t.Fatal(err)
	}
	pins, ok := client["certificate_public_key_sha256"].([]interface{})
	if !ok || len(pins) != 1 || pins[0] != expectedPin {
		t.Fatalf("pin = %#v, want %q", client["certificate_public_key_sha256"], expectedPin)
	}
	if _, ok := client["certificate"]; ok {
		t.Fatal("client certificate must be removed when public-key pin is set")
	}
	if _, ok := client["certificate_path"]; ok {
		t.Fatal("client certificate_path must be removed when public-key pin is set")
	}
}

func TestApplySelfSignedPublicKeyPinSkipsReality(t *testing.T) {
	certPEM, _ := testSelfSignedCert(t)
	tls := model.Tls{
		Server: json.RawMessage(`{"enabled":true,"certificate":` + mustJSON(t, []string{certPEM}) + `,"reality":{"enabled":true}}`),
		Client: json.RawMessage(`{"certificate_public_key_sha256":["stale"]}`),
	}

	if !ApplySelfSignedPublicKeyPin(&tls) {
		t.Fatal("expected stale pin to be removed for Reality TLS")
	}
	var client map[string]interface{}
	if err := json.Unmarshal(tls.Client, &client); err != nil {
		t.Fatal(err)
	}
	if _, ok := client["certificate_public_key_sha256"]; ok {
		t.Fatal("Reality TLS must not keep certificate_public_key_sha256")
	}
}

func testSelfSignedCert(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	sum := x509SHA256(cert.RawSubjectPublicKeyInfo)
	return string(pemBytes), base64.StdEncoding.EncodeToString(sum[:])
}

func x509SHA256(data []byte) [32]byte {
	return sha256Sum(data)
}

func sha256Sum(data []byte) [32]byte {
	// Kept as a tiny wrapper so the expected value path is explicit in the test.
	return sha256.Sum256(data)
}

func mustJSON(t *testing.T, value interface{}) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
