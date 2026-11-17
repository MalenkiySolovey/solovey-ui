package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
)

func TestLogicalFileManifestRejectsCorruption(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "files.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.AutoMigrate(&OwnerFile{}); err != nil {
		t.Fatal(err)
	}
	data := []byte("durable file fixture")
	sum := sha256.Sum256(data)
	f := OwnerFile{Owner: "test-owner", Key: "asset:1", Data: data, Digest: hex.EncodeToString(sum[:])}
	if err := db.Create(&f).Error; err != nil {
		t.Fatal(err)
	}
	d, err := DigestTable(context.Background(), db, "core", BackupFileTable)
	if err != nil {
		t.Fatal(err)
	}
	m := &FileBackupManifest{Schema: "solovey.logical-owner-files/v1", Owners: []string{"test-owner"}, Rows: d.Rows, SchemaDigest: d.SchemaDigest, ContentDigest: d.ContentDigest}
	if err := VerifyOwnerFiles(context.Background(), db, m); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&OwnerFile{}).Where("owner = ?", f.Owner).Update("data", []byte("corrupt")).Error; err != nil {
		t.Fatal(err)
	}
	if err := VerifyOwnerFiles(context.Background(), db, m); err == nil {
		t.Fatal("tampered payload accepted")
	}
}
