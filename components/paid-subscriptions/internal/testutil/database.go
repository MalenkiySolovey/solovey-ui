//go:build !minimal

package testutil

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"gorm.io/gorm"
)

func OpenDatabase(t testing.TB) *gorm.DB {
	t.Helper()
	if err := service.StopAuditWriter(context.Background()); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", folder)
	// Match the production WAL database. Shared-cache in-memory SQLite cannot
	// use WAL and returns SQLITE_LOCKED immediately when the asynchronous audit
	// writer overlaps a read, even with the configured busy timeout.
	if err := dbsqlite.Init(filepath.Join(folder, "paid-test.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	db := dbsqlite.DB()
	t.Cleanup(func() {
		if err := service.StopAuditWriter(context.Background()); err != nil {
			t.Errorf("stop audit writer: %v", err)
		}
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}
