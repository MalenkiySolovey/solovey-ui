//go:build linux

package artifacts

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func validateStorageRootOwnership(root string, mode os.FileMode) error {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode {
		return fmt.Errorf("%w: runtime root type or mode is unsafe", ErrPathForbidden)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() {
		return errors.New("artifact runtime root is not owned by the service identity")
	}
	return nil
}
