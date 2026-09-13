//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"

	openwrt "github.com/MalenkiySolovey/solovey-ui/deploy/openwrt"
	broker "github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "solovey broker readiness:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 1 {
		return errors.New("arguments are not supported")
	}
	return runWith(readinessRuntime{
		euid: os.Geteuid,
		validate: func() error {
			_, err := openwrt.InspectDatabaseDurableState(1)
			return err
		},
		wait: func() error {
			return broker.WaitForReadiness(context.Background(), broker.DefaultReadinessTimeout)
		},
		exec: syscall.Exec,
	})
}

type readinessRuntime struct {
	euid     func() int
	validate func() error
	wait     func() error
	exec     func(string, []string, []string) error
}

func runWith(runtime readinessRuntime) error {
	if runtime.euid == nil || runtime.validate == nil || runtime.wait == nil || runtime.exec == nil {
		return errors.New("broker readiness runtime is incomplete")
	}
	if runtime.euid() == 0 {
		return errors.New("broker readiness must run as the panel service account")
	}
	if err := runtime.validate(); err != nil {
		return fmt.Errorf("OpenWrt package authority validation: %w", err)
	}
	if err := runtime.wait(); err != nil {
		return err
	}
	return runtime.exec(openwrt.PanelExecutablePath, []string{openwrt.PanelExecutablePath}, os.Environ())
}
