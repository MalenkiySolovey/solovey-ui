//go:build linux

package main

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

// execPackageProcess retains the procd main PID and credentials. In pinned
// procd, a bare no_new_privs parameter is only enforced on the ujail path;
// these package instances deliberately use direct exec instead.
func execPackageProcess(executable string, arguments, environment []string) error {
	// The kernel bit is thread-local. Set, read back and exec on the same OS
	// thread so the replacement image (and every thread it creates) inherits it.
	// Do not unlock on failure: the bit cannot be undone, and main must exit
	// rather than return this hardened thread to unrelated Go work.
	runtime.LockOSThread()
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("OpenWrt package exec cannot set no_new_privs: %w", err)
	}
	active, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if err != nil {
		return fmt.Errorf("OpenWrt package exec cannot verify no_new_privs: %w", err)
	}
	if active != 1 {
		return errors.New("OpenWrt package exec kernel no_new_privs is not active")
	}
	return syscall.Exec(executable, arguments, environment)
}
