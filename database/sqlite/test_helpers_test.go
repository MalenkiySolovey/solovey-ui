package sqlite

import (
	"os"
	"testing"
	"time"
)

func cleanupSQLiteSidecars(path string) {
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-shm")
	_ = os.Remove(path + "-journal")
}

func closeMainDB(t *testing.T) {
	t.Helper()
	current := DB()
	if current == nil {
		return
	}
	_ = current.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error
	if err := Close(); err != nil {
		t.Logf("close database: %v", err)
	}
}

func makeDBTempDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var removeErr error
		for attempt := 0; attempt < 20; attempt++ {
			removeErr = os.RemoveAll(dir)
			if removeErr == nil || os.IsNotExist(removeErr) {
				return
			}
			time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
		}
		t.Errorf("remove temp database directory %q: %v", dir, removeErr)
	})
	return dir
}
