//go:build linux

package privilegedbroker

import (
	"errors"
	"os"
	"strings"
)

func currentBootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	value := strings.TrimSpace(string(data))
	if err != nil || !safeIdentifier("boot-"+value) {
		return "", errors.New("kernel boot identity is invalid")
	}
	return value, nil
}
