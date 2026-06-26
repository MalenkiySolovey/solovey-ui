//go:build !windows

package bind

import (
	"errors"
	"syscall"
)

func isAddrNotAvailable(err error) bool {
	return errors.Is(err, syscall.EADDRNOTAVAIL)
}
