//go:build !minimal

package jobs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	ipmonitor "github.com/MalenkiySolovey/solovey-ui/ipmonitor"

	"github.com/robfig/cron/v3"
)

type testEntryScheduler struct {
	*cron.Cron
}

func (s testEntryScheduler) RemoveJob(id cron.EntryID) {
	s.Remove(id)
}

func (s testEntryScheduler) RemoveJobAndWait(_ context.Context, id cron.EntryID) error {
	s.Remove(id)
	return nil
}

func initDatabase(t *testing.T) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "s-ui-telegram-jobs-test-")
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

func removeTempDir(t *testing.T, dir string) {
	t.Helper()
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		err = os.RemoveAll(dir)
		if err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	t.Errorf("remove telegram jobs temp dir %q: %v", dir, err)
}
