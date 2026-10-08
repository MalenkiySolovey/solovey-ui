package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	"github.com/caddyserver/certmagic"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func certificateProviderDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "provider-files.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Setting{}, &model.Tls{}, &model.TLSCertificateProvider{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLogicalACMEFilesRestorePortableEncryptedAndLegacy(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "encrypted", true: "old-inline"}[legacy], func(t *testing.T) {
			ctx := context.Background()
			source := certificateProviderDB(t)
			oldRoot := t.TempDir()
			options, _ := json.Marshal(map[string]any{"domain": []string{"fixture.invalid"}, "email": "operator@fixture.invalid", "data_directory": oldRoot, "external_account": map[string]string{"key_id": "fixture-eab", "mac_key": "synthetic-account-intent"}})
			definition := entitytls.ProviderDefinition{Tag: "tls-acme-1", Type: "acme", RuntimeMode: entitytls.LegacyInlineMode, Options: options}
			server := json.RawMessage(`{"enabled":false,"certificate_provider":"tls-acme-1"}`)
			if legacy {
				server, _ = json.Marshal(map[string]any{"enabled": false, "acme": json.RawMessage(options)})
			} else if err := entitytls.WriteProviderDefinitions(source, []entitytls.ProviderDefinition{definition}); err != nil {
				t.Fatal(err)
			}
			profile := model.Tls{Id: 1, Name: "fixture", Server: server, Client: json.RawMessage(`{"enabled":false,"insecure":false}`)}
			if err := source.Create(&profile).Error; err != nil {
				t.Fatal(err)
			}
			issuer := (&certmagic.ACMEIssuer{CA: certmagic.LetsEncryptProductionCA}).IssuerKey()
			accountPrefix := "acme/" + certmagic.StorageKeys.Safe(issuer) + "/users/operator@fixture.invalid/operator"
			private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			keyPEM, err := certmagic.PEMEncodePrivateKey(private)
			if err != nil {
				t.Fatal(err)
			}
			payloads := map[string][]byte{
				accountPrefix + ".json": []byte(`{"status":"valid","contact":["mailto:operator@fixture.invalid"],"location":"https://ca.fixture.invalid/account/1"}`),
				accountPrefix + ".key":  keyPEM,
				certmagic.StorageKeys.SiteCert(issuer, "fixture.invalid"):       []byte("-----BEGIN CERTIFICATE-----\nZml4dHVyZQ==\n-----END CERTIFICATE-----\n"),
				certmagic.StorageKeys.SitePrivateKey(issuer, "fixture.invalid"): keyPEM,
				certmagic.StorageKeys.SiteMeta(issuer, "fixture.invalid"):       []byte(`{"sans":["fixture.invalid"]}`),
			}
			storage := &certmagic.FileStorage{Path: oldRoot}
			for key, data := range payloads {
				if err := storage.Store(ctx, key, data); err != nil {
					t.Fatal(err)
				}
			}
			if err := storage.Store(ctx, "unrelated/sensitive.data", []byte("unrelated-owned-state")); err != nil {
				t.Fatal(err)
			}
			files, err := exportCertificateFiles(ctx, source)
			if err != nil || len(files) != 6 {
				t.Fatal("managed ACME export did not include exact account pair, certificate trio and manifest")
			}
			for _, file := range files {
				if strings.Contains(file.Key, "unrelated") || strings.Contains(file.Key, oldRoot) {
					t.Fatal("arbitrary source path or file entered the archive")
				}
			}
			candidate := certificateProviderDB(t)
			var settings []model.Setting
			var records []model.TLSCertificateProvider
			if err := source.Find(&settings).Error; err != nil {
				t.Fatal(err)
			}
			if err := source.Find(&records).Error; err != nil {
				t.Fatal(err)
			}
			for _, setting := range settings {
				if err := candidate.Create(&setting).Error; err != nil {
					t.Fatal(err)
				}
			}
			for _, record := range records {
				if err := candidate.Create(&record).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := candidate.Create(&profile).Error; err != nil {
				t.Fatal(err)
			}
			target := t.TempDir()
			t.Setenv("SUI_DB_FOLDER", target)
			if err := restoreCertificateFiles(ctx, candidate, files, false); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(target, "certs")); !os.IsNotExist(err) {
				t.Fatal("rehearsal published managed files")
			}
			if err := restoreCertificateFiles(ctx, candidate, files, true); err != nil {
				t.Fatal(err)
			}
			var restored model.Tls
			if err := candidate.First(&restored, profile.Id).Error; err != nil {
				t.Fatal(err)
			}
			var restoredOptions json.RawMessage
			if legacy {
				var fields map[string]json.RawMessage
				_ = json.Unmarshal(restored.Server, &fields)
				restoredOptions = fields["acme"]
				var count int64
				candidate.Model(&model.TLSCertificateProvider{}).Count(&count)
				if count != 0 {
					t.Fatal("backup restore activated a partial provider migration")
				}
			} else {
				definitions, err := entitytls.ReadProviderDefinitions(candidate)
				if err != nil || len(definitions) != 1 {
					t.Fatal("portable encrypted provider could not be opened")
				}
				restoredOptions = definitions[0].Options
				if !strings.Contains(string(restoredOptions), "synthetic-account-intent") {
					t.Fatal("encrypted provider secret intent lost")
				}
			}
			var adapter struct {
				Directory string `json:"data_directory"`
			}
			_ = json.Unmarshal(restoredOptions, &adapter)
			if adapter.Directory == oldRoot || !strings.HasPrefix(adapter.Directory, target) {
				t.Fatal("source directory remained authoritative")
			}
			reboundStorage := &certmagic.FileStorage{Path: adapter.Directory}
			for key, data := range payloads {
				actual, err := reboundStorage.Load(ctx, key)
				if err != nil || string(actual) != string(data) {
					t.Fatal("pinned FileStorage could not read restored logical data")
				}
			}
			accountIssuer := certmagic.NewACMEIssuer(&certmagic.Config{Storage: reboundStorage}, certmagic.ACMEIssuer{CA: certmagic.LetsEncryptProductionCA, Email: "operator@fixture.invalid"})
			account, err := accountIssuer.GetAccount(ctx, keyPEM)
			if err != nil || account.PrivateKey == nil || account.Location != "https://ca.fixture.invalid/account/1" {
				t.Fatal("pinned certmagic could not recover restored account without issuance")
			}
			var unchanged model.Tls
			if err := source.First(&unchanged, profile.Id).Error; err != nil || string(unchanged.Server) != string(profile.Server) {
				t.Fatal("source DB changed during candidate restore")
			}
			bad := append([]backup.OwnerFile(nil), files...)
			bad = append(bad, backup.OwnerFile{Key: "provider:tls-acme-1:file:../../outside", Data: keyPEM})
			if err := restoreCertificateFiles(ctx, candidate, bad, false); err == nil {
				t.Fatal("archive-authorized output path accepted")
			}
			if err := restoreCertificateFiles(ctx, candidate, files[:len(files)-1], false); err == nil {
				t.Fatal("missing managed inventory accepted")
			}
			// A missing member in the source inventory is a stable manual state,
			// rather than permission to omit a required private file.
			if err := os.Remove(filepath.Join(oldRoot, filepath.FromSlash(accountPrefix+".key"))); err != nil {
				t.Fatal(err)
			}
			if _, err := exportCertificateFiles(ctx, source); err == nil || !strings.Contains(err.Error(), "TLS_PROVIDER_FILE_MISSING") {
				t.Fatal("incomplete source account was silently backed up")
			}
		})
	}
}
