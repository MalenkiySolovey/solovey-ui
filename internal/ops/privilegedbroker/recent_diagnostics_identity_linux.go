//go:build linux

package privilegedbroker

import (
	"os"
	"syscall"
)

func diagnosticCurrentUID() (uint32, bool) {
	return uint32(os.Geteuid()), true
}

func diagnosticFileUIDFromInfo(info os.FileInfo) (uint32, bool) {
	if info == nil {
		return 0, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return stat.Uid, ok
}
