//go:build linux

package evidencebundle

import (
	"errors"
	"os"
	"syscall"
)

func validateEvidenceOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return errors.New("evidence object is not owned by root")
	}
	return nil
}

func validateEvidencePermissions(info os.FileInfo) error {
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("evidence object permissions are not private")
	}
	return nil
}
