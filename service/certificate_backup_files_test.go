package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLogicalCertificateFilesRestoreUsesOwnerDestination(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "certificate.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.AutoMigrate(&model.Setting{}, &model.Tls{}); err != nil {
		t.Fatal(err)
	}
	data := []byte("-----BEGIN CERTIFICATE-----\nZml4dHVyZQ==\n-----END CERTIFICATE-----\n")
	old := filepath.Join(t.TempDir(), "cert.pem")
	if err := os.WriteFile(old, data, 0o600); err != nil {
		t.Fatal(err)
	}
	setting := model.Setting{Key: "webCertFile", Value: old}
	if err := db.Create(&setting).Error; err != nil {
		t.Fatal(err)
	}
	files, err := exportCertificateFiles(context.Background(), db)
	if err != nil || len(files) != 1 {
		t.Fatalf("export: %v", err)
	}
	target := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", target)
	if err := restoreCertificateFiles(context.Background(), db, files, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "certs")); !os.IsNotExist(err) {
		t.Fatal("rehearsal published certificate")
	}
	if err := restoreCertificateFiles(context.Background(), db, files, true); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("key = ?", setting.Key).Take(&setting).Error; err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(setting.Value)
	if err != nil || string(restored) != string(data) {
		t.Fatalf("restore: %v", err)
	}
	if setting.Value == old {
		t.Fatal("archive source path reused")
	}
	info, err := os.Stat(setting.Value)
	if err != nil || info.Size() != int64(len(data)) {
		t.Fatal("certificate publication missing")
	}
	if err := restoreCertificateFiles(context.Background(), db, nil, false); err == nil {
		t.Fatal("missing certificate accepted")
	}
}

func TestLogicalMTLSFilesRestoreKeepsSideShapeAndPreflight(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mtls.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.AutoMigrate(&model.Setting{}, &model.Tls{}); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, filename := range []string{"ca-a", "ca-b", "client-cert", "client-key"} {
		name := filepath.Join(t.TempDir(), filename)
		if err := os.WriteFile(name, []byte("-----BEGIN CERTIFICATE-----\nZml4dHVyZQ==\n-----END CERTIFICATE-----\n"), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, name)
	}
	server, _ := json.Marshal(map[string]any{"enabled": false, "client_certificate_path": paths[:2], "client_authentication": "require-and-verify"})
	client, _ := json.Marshal(map[string]any{"enabled": false, "client_certificate_path": paths[2], "client_key_path": paths[3], "insecure": false})
	profile := model.Tls{Name: "fixture", Server: server, Client: client}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	files, err := exportCertificateFiles(context.Background(), db)
	if err != nil || len(files) != 4 {
		t.Fatal("mTLS inventory did not include the server CA list and client pair")
	}
	target := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", target)
	if err := restoreCertificateFiles(context.Background(), db, files, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "certs")); !os.IsNotExist(err) {
		t.Fatal("preflight published files")
	}
	if err := restoreCertificateFiles(context.Background(), db, files, true); err != nil {
		t.Fatal(err)
	}
	var restored model.Tls
	if err := db.First(&restored, profile.Id).Error; err != nil {
		t.Fatal(err)
	}
	var afterServer struct {
		Paths []string `json:"client_certificate_path"`
	}
	var afterClient struct {
		Certificate string `json:"client_certificate_path"`
		Key         string `json:"client_key_path"`
		Insecure    bool   `json:"insecure"`
	}
	if json.Unmarshal(restored.Server, &afterServer) != nil || json.Unmarshal(restored.Client, &afterClient) != nil || len(afterServer.Paths) != 2 {
		t.Fatal("mTLS side path shapes changed")
	}
	for _, name := range append(afterServer.Paths, afterClient.Certificate, afterClient.Key) {
		if !strings.HasPrefix(name, target) {
			t.Fatal("archive source path reused")
		}
		if _, err := os.ReadFile(name); err != nil {
			t.Fatal("managed file unavailable")
		}
	}
	bad := append(files, files[0])
	if err := restoreCertificateFiles(context.Background(), db, bad, false); err == nil {
		t.Fatal("duplicate inventory admitted")
	}
}
