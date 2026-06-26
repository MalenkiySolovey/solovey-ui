package testutil

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"gorm.io/gorm"
)

var databaseSequence atomic.Int64

func OpenDatabase(t testing.TB) *gorm.DB {
	t.Helper()
	if err := service.StopAuditWriter(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUI_DB_FOLDER", t.TempDir())

	dsn := fmt.Sprintf("file:paidsub_test_%d?mode=memory&cache=shared", databaseSequence.Add(1))
	if err := dbsqlite.Init(dsn); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove("initial-admin.txt") })
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
