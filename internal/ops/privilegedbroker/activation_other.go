//go:build !linux

package privilegedbroker

import (
	"errors"
	"net"
)

func ActivatedListeners() (map[Role]*net.UnixListener, error) {
	return nil, errors.New("privileged broker requires Linux systemd socket activation")
}
