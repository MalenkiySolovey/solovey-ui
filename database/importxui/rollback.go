package importxui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func ValidateRollbackBackupPath(path, databasePath string) error {
	if path == "" {
		return errors.New("missing backup path")
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("invalid backup path")
	}
	pathDir, err := filepath.Abs(filepath.Dir(abs))
	if err != nil {
		return err
	}
	baseDir, err := filepath.Abs(filepath.Dir(databasePath))
	if err != nil {
		return err
	}
	if pathDir != baseDir || !strings.HasPrefix(filepath.Base(abs), "s-ui-pre-xui-import-") || filepath.Ext(abs) != ".db" {
		return errors.New("invalid backup path")
	}
	return nil
}
