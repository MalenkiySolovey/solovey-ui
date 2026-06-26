package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	ipmonitor "github.com/MalenkiySolovey/solovey-ui/ipmonitor"
)

// TB is the subset of testing.T used by scheduled-job integration tests.
type TB interface {
	Helper()
	Fatal(args ...any)
	Errorf(format string, args ...any)
	Skip(args ...any)
	Setenv(key, value string)
	Cleanup(func())
}

func InitDatabase(t TB) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "s-ui-cronjob-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUI_DB_FOLDER", tempDir)
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(filepath.Join(tempDir, "s-ui.db")); err != nil {
		removeTempDir(t, tempDir)
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if current := dbsqlite.DB(); current != nil {
			_ = current.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error
		}
		_ = dbsqlite.Close()
		ipmonitor.InvalidateAllCache()
		removeTempDir(t, tempDir)
	})
}

func removeTempDir(t TB, dir string) {
	t.Helper()
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		err = os.RemoveAll(dir)
		if err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	t.Errorf("remove cronjob temp dir %q: %v", dir, err)
}
