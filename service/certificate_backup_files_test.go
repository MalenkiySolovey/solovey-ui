package service

import (
	"context"
	"os"
	"path/filepath"
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
