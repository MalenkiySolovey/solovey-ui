//go:build !linux

package privilegedbroker

import "runtime"

func currentBootID() (string, error) { return "nonlinux-" + runtime.GOOS, nil }
