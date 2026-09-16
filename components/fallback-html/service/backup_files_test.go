//go:build !minimal

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	domain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLogicalFileBackupReconstructsFallbackAssets(t *testing.T) {
	ctx := context.Background()
	sourceRoot, targetRoot := t.TempDir(), t.TempDir()
	t.Setenv("SUI_DB_FOLDER", sourceRoot)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "owner.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.AutoMigrate(&domain.Asset{}); err != nil {
		t.Fatal(err)
	}
	data := []byte("body { color: blue; }")
	sum := sha256.Sum256(data)
	name := filepath.Join(assetRoot(1), "source.css")
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatal(err)
	}
	asset := domain.Asset{ID: 1, SiteID: 1, LogicalPath: "/media/test.css", FilePath: name, Sha256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(data)), MimeType: "text/css"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	files, err := exportFallbackFiles(ctx, db)
	if err != nil || len(files) != 1 {
		t.Fatalf("export %v %d", err, len(files))
	}
	files[0].Owner, files[0].Digest = "fallback-html", asset.Sha256
	t.Setenv("SUI_DB_FOLDER", targetRoot)
	if err := restoreFallbackFiles(ctx, db, files, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(storageRoot()); !os.IsNotExist(err) {
		t.Fatal("rehearsal wrote live files")
	}
	if err := restoreFallbackFiles(ctx, db, files, true); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&asset, 1).Error; err != nil {
		t.Fatal(err)
	}
	restored, err := readOwnedRegularFile(assetRoot(1), asset.FilePath)
	if err != nil || string(restored) != string(data) {
		t.Fatalf("restored %v", err)
	}
	if _, err := os.Stat(name); err != nil {
		t.Fatal("original rollback data changed")
	}
	if err := restoreFallbackFiles(ctx, db, nil, false); err == nil {
		t.Fatal("missing asset accepted")
	}
	bad := append([]backup.OwnerFile(nil), files...)
	bad[0].Key = "asset:999"
	if err := restoreFallbackFiles(ctx, db, bad, false); err == nil {
		t.Fatal("unknown file accepted")
	}
}
