//go:build !windows

package helper

import (
	"os"
	"syscall"
)

func platformRootOwned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0
}
