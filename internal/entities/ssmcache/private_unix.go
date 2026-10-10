//go:build !windows

package ssmcache

import (
	"errors"
	"os"
	"syscall"
)

func secureCreated(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	mode := os.FileMode(0600)
	if info.IsDir() {
		mode = 0700
	}
	return f.Chmod(mode)
}

func checkPrivate(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0077 != 0 || (!info.IsDir() && stat.Nlink != 1) {
		return errors.New("SSM_CACHE_STORAGE_NOT_PRIVATE")
	}
	return nil
}
