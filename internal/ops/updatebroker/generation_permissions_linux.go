//go:build linux

package updatebroker

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// The Root service keeps UMask=0077 for private state. Published executable
// ancestry has its own exact public mode, applied to the newly created object.
func createUpdateDirectory(path string, mode os.FileMode) error {
	if err := os.Mkdir(path, mode); err != nil {
		return err
	}
	directory, err := os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	modeErr := directory.Chmod(mode.Perm())
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if err := errors.Join(modeErr, syncErr, closeErr); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func ensureReleaseParentDirectory(root, parent string) error {
	if filepath.Clean(parent) == filepath.Clean(root) {
		return nil
	}
	// The canonical archive accepts only top-level files and systemd members.
	if filepath.Dir(parent) != filepath.Clean(root) || filepath.Base(parent) != "systemd" {
		return errors.New("release member parent is outside the package layout")
	}
	return ensureUpdateDirectory(parent, 0o755)
}
