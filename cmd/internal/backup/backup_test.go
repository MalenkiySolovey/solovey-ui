package backupcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRunWritesBackupFile(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dbDir)
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { closeDB(t) })
	if err := dbsqlite.DB().Create(&model.Client{Name: "cli-backup", Inbounds: []byte("[]"), Links: []byte("[]"), Config: []byte("{}")}).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(t.TempDir(), "backup.db")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"-output", output}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run exit code=%d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	backupDB, err := gorm.Open(sqlite.Open(output), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if sqlDB, err := backupDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()
	var count int64
	if err := backupDB.Model(&model.Client{}).Where("name = ?", "cli-backup").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("backup client count=%d, want 1", count)
	}
}

func TestRunRequiresOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("Run exit code=%d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "-output is required") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func closeDB(t *testing.T) {
	t.Helper()
	if db := dbsqlite.DB(); db != nil {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
	_ = dbsqlite.Close()
	_ = os.Remove(configstorage.GetDBPath())
}
