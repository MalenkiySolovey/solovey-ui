//go:build !minimal

package importxui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

func WritePreImportBackup(now int64) (string, error) {
	if now == 0 {
		now = time.Now().Unix()
	}
	staged, cleanup, err := backup.PrepareExport("")
	if err != nil {
		return "", fmt.Errorf("xui-import: %w", err)
	}
	defer cleanup()
	dir := filepath.Dir(configstorage.GetDBPath())
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("xui-import: %w", err)
	}
	path := ""
	for suffix := 0; suffix < 100; suffix++ {
		name := fmt.Sprintf("s-ui-pre-xui-import-%d.db", now)
		if suffix > 0 {
			name = fmt.Sprintf("s-ui-pre-xui-import-%d-%02d.db", now, suffix)
		}
		candidate := filepath.Join(dir, name)
		if _, statErr := os.Lstat(candidate); os.IsNotExist(statErr) {
			path = candidate
			break
		} else if statErr != nil {
			return "", fmt.Errorf("xui-import: %w", statErr)
		}
	}
	if path == "" {
		return "", errors.New("xui-import: pre-import backup name inventory is exhausted")
	}
	if err := os.Rename(staged, path); err != nil {
		return "", fmt.Errorf("xui-import: %w", err)
	}
	logger.Info("xui-import: pre-import backup saved to ", path)
	prunePreImportBackups(dir, preImportBackupRetention)
	return path, nil
}

// preImportBackupRetention bounds how many s-ui-pre-xui-import-*.db files are
// kept. Every import writes one, and a slow import behind a client/proxy that
// resubmits can produce dozens, filling the db directory.
const preImportBackupRetention = 10

// prunePreImportBackups removes all but the newest keep pre-import backups in
// dir. The filenames embed a fixed-width unix timestamp, so a lexical sort is
// chronological. Best-effort: failures are logged, not fatal to the import.
func prunePreImportBackups(dir string, keep int) {
	matches, err := filepath.Glob(filepath.Join(dir, "s-ui-pre-xui-import-*.db"))
	if err != nil || len(matches) <= keep {
		return
	}
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-keep] {
		if err := os.Remove(old); err != nil {
			logger.Warning("xui-import: failed to prune old pre-import backup ", old, ": ", err)
		}
	}
}
