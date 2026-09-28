//go:build ignore

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/logger"
	"golang.org/x/sys/unix"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if os.Geteuid() == 0 {
		return fmt.Errorf("probe must be unprivileged")
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return err
	}
	for _, key := range []string{"CapInh:", "CapPrm:", "CapEff:", "CapBnd:", "CapAmb:"} {
		found := false
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, key) {
				found = true
				if strings.TrimSpace(strings.TrimPrefix(line, key)) != "0000000000000000" {
					return fmt.Errorf("nonzero %s", key)
				}
			}
		}
		if !found {
			return fmt.Errorf("missing %s", key)
		}
	}
	if !strings.Contains(string(status), "NoNewPrivs:\t1") {
		return fmt.Errorf("NoNewPrivileges missing")
	}
	monitor, err := tun.NewNetworkUpdateMonitor(logger.NOP())
	if err != nil {
		return err
	}
	err = monitor.Start()
	if len(os.Args) == 2 && os.Args[1] == "blocked" {
		if !errors.Is(err, unix.EAFNOSUPPORT) || !strings.Contains(err.Error(), "subscribe route updates") {
			return fmt.Errorf("expected original route subscription EAFNOSUPPORT, got %v", err)
		}
		fmt.Println("ORIGINAL_ROUTE_SUBSCRIPTION_EAFNOSUPPORT=PASS")
		return nil
	}
	if err != nil {
		return err
	}
	defer monitor.Close()
	for _, family := range []int{unix.AF_INET, unix.AF_INET6, unix.AF_UNIX} {
		fd, err := unix.Socket(family, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			return fmt.Errorf("required family %d: %w", family, err)
		}
		unix.Close(fd)
	}
	for _, family := range []int{unix.AF_PACKET, unix.AF_BLUETOOTH, unix.AF_VSOCK, unix.AF_XDP} {
		fd, err := unix.Socket(family, unix.SOCK_RAW|unix.SOCK_CLOEXEC, 0)
		if err == nil {
			unix.Close(fd)
			return fmt.Errorf("unnecessary family %d allowed", family)
		}
		if !errors.Is(err, unix.EAFNOSUPPORT) {
			return fmt.Errorf("family %d not denied by address-family sandbox: %w", family, err)
		}
	}
	fmt.Println("PINNED_SING_TUN_ROUTE_LINK_MONITOR=PASS UID_NONROOT=PASS CAPABILITIES_ZERO=PASS REQUIRED_FAMILIES=PASS UNNECESSARY_FAMILIES_BLOCKED=PASS")
	return nil
}
