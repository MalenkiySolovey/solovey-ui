//go:build !minimal

package importxui

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	tun "github.com/sagernet/sing-tun"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Runtime tests need valid certificate material. Mapping-only tests retain
// their opaque PEM placeholders because they do not construct the core.
func runtimeTLSFixtureStream(t *testing.T, serverName string) string {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{serverName}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	stream, err := json.Marshal(map[string]any{
		"network": "tcp", "security": "tls",
		"tlsSettings": map[string]any{
			"serverName":   serverName,
			"certificates": []any{map[string]any{"certificate": []string{certPEM}, "key": []string{keyPEM}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(stream)
}

// Schema/routing/plain-TLS tests use ordinary TLS and the existing no-peers
// WireGuard exclusion. Dedicated regressions below qualify option-dependent
// acceptance and rollback instead of bypassing official validation.
func buildRuntimeCompatSource(t *testing.T, variant schemaVariant, path string) {
	t.Helper()
	buildCompatSource(t, variant, path)
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	stream := runtimeTLSFixtureStream(t, "runtime-fixture.example")
	if err := db.Exec("UPDATE inbounds SET stream_settings = ? WHERE protocol IN ('vless','trojan')", stream).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE inbounds SET settings = ? WHERE protocol = 'wireguard'", `{"peers":[]}`).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCapabilityImportUserspaceWireGuardHonorsOfficialProfile(t *testing.T) {
	initPlanExtraMainDB(t)
	before := tableCounts(t, "inbounds", "endpoints", "tls", "clients", "audit_events")
	key := make([]byte, 32)
	key[0] = 1
	encoded := base64.StdEncoding.EncodeToString(key)
	settings, err := json.Marshal(map[string]any{"secretKey": encoded, "peers": []any{map[string]any{"publicKey": encoded, "allowedIPs": []string{"0.0.0.0/0"}}}})
	if err != nil {
		t.Fatal(err)
	}
	row := validPlanExtraInbound()
	row.protocol, row.settings = "wireguard", string(settings)
	src := createPlanExtraSource(t, []planExtraInbound{row})
	plan, err := Plan(src, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	report, err := Apply(src, *plan, ApplyOptions{SkipBackup: true, SkipAudit: true})
	if tun.WithGVisor {
		if err != nil {
			t.Fatal(err)
		}
		if report.Summary.Endpoints.Imported != 1 {
			t.Fatal("supported userspace device was not imported")
		}
		return
	}
	if err == nil || !strings.HasSuffix(err.Error(), "gVisor is not included in this build, rebuild with -tags with_gvisor") {
		t.Fatal("official option-dependent rejection was bypassed")
	}
	after := tableCounts(t, "inbounds", "endpoints", "tls", "clients", "audit_events")
	for table, count := range before {
		if after[table] != count {
			t.Fatalf("failed official validation committed %s", table)
		}
	}
}

func assertRealityProfileImport(t *testing.T, supported bool) {
	t.Helper()
	initPlanExtraMainDB(t)
	before := tableCounts(t, "inbounds", "endpoints", "tls", "clients", "audit_events")
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := json.Marshal(map[string]any{
		"network": "tcp", "security": "reality",
		"realitySettings": map[string]any{"serverNames": []string{"fixture.example"}, "dest": "fixture.example:443", "privateKey": base64.RawURLEncoding.EncodeToString(privateKey.Bytes()), "publicKey": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes()), "shortIds": []string{"01234567"}, "fingerprint": "chrome"},
	})
	if err != nil {
		t.Fatal(err)
	}
	src := createPlanExtraSource(t, []planExtraInbound{validPlanExtraInbound()})
	db, err := gorm.Open(sqlite.Open(src), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE inbounds SET stream_settings = ?", string(stream)).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	plan, err := Plan(src, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	report, err := Apply(src, *plan, ApplyOptions{SkipBackup: true, SkipAudit: true})
	if supported {
		if err != nil {
			t.Fatal(err)
		}
		if report.Summary.Inbounds.Imported != 1 || report.Summary.TLS.Created != 1 {
			t.Fatal("supported Reality candidate was not imported")
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), "uTLS, which is required by reality is not included in this build") {
		t.Fatal("official Reality profile rejection was bypassed")
	}
	after := tableCounts(t, "inbounds", "endpoints", "tls", "clients", "audit_events")
	for table, count := range before {
		if after[table] != count {
			t.Fatalf("failed Reality validation committed %s", table)
		}
	}
}
