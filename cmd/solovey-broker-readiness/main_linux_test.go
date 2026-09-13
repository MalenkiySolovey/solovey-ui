//go:build linux

package main

import (
	"errors"
	"reflect"
	"testing"

	openwrt "github.com/MalenkiySolovey/solovey-ui/deploy/openwrt"
)

func TestReadinessSuccessHandsTheSameProcdPIDToPanelExec(t *testing.T) {
	steps := make([]string, 0, 3)
	var path string
	var arguments []string
	err := runWith(readinessRuntime{
		euid: func() int { return 1001 },
		validate: func() error {
			steps = append(steps, "validate")
			return nil
		},
		wait: func() error {
			steps = append(steps, "attest")
			return nil
		},
		exec: func(observedPath string, observedArguments, _ []string) error {
			steps = append(steps, "exec")
			path, arguments = observedPath, append([]string(nil), observedArguments...)
			return nil
		},
	})
	if err != nil || !reflect.DeepEqual(steps, []string{"validate", "attest", "exec"}) ||
		path != openwrt.PanelExecutablePath || !reflect.DeepEqual(arguments, []string{openwrt.PanelExecutablePath}) {
		t.Fatalf("handoff err=%v steps=%v path=%q arguments=%v", err, steps, path, arguments)
	}
}

func TestReadinessFailureNeverExecsPanel(t *testing.T) {
	executed := false
	err := runWith(readinessRuntime{
		euid:     func() int { return 1001 },
		validate: func() error { return nil },
		wait:     func() error { return errors.New("not authorized") },
		exec: func(string, []string, []string) error {
			executed = true
			return nil
		},
	})
	if err == nil || executed {
		t.Fatalf("failed attestation err=%v executed=%t", err, executed)
	}
}
