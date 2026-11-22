//go:build !linux

package privilegedbroker

import (
	"errors"
	"net"
)

func ActivatedListeners() (map[Role]*net.UnixListener, zapretDenyIdentity, error) {
	return nil, zapretDenyIdentity{}, errors.New("privileged broker requires Linux systemd socket activation")
}
