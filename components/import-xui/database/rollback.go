//go:build !minimal

package importxui

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var rollbackBackupName = regexp.MustCompile(`^s-ui-pre-xui-import-[0-9]+(?:-[0-9]{2})?\.db$`)

// ResolveRollbackBackupPath converts the opaque backup reference returned by
// Apply into a server-local path. Absolute paths and path components are never
// accepted from the HTTP client.
func ResolveRollbackBackupPath(reference, databasePath string) (string, error) {
	if reference == "" {
		return "", errors.New("missing backup reference")
	}
	if filepath.Base(reference) != reference || strings.ContainsAny(reference, `/\`) || !rollbackBackupName.MatchString(reference) {
		return "", errors.New("invalid backup reference")
	}
	baseDir, err := filepath.Abs(filepath.Dir(databasePath))
	if err != nil {
		return "", err
	}
	abs := filepath.Join(baseDir, reference)
	info, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("invalid backup reference")
	}
	return abs, nil
}
